package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	fishingModeManual  = "manual"
	fishingModeByLevel = "por-nivel"
)

// normalizeFishingMode preserva o comportamento anterior para clientes que
// ainda não enviam o campo mode.
func normalizeFishingMode(mode string) (string, bool) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return fishingModeManual, true
	}
	switch mode {
	case fishingModeManual, fishingModeByLevel:
		return mode, true
	default:
		return "", false
	}
}

func fishingDestinationForLevel(level int) string {
	switch {
	case level >= 70:
		return "snouthill-pier"
	case level >= 30:
		return "jardim-flutuante"
	default:
		// Sem leitura anterior, a área inicial é a opção segura.
		return "infobus"
	}
}

type fishingReportLevelSnapshot struct {
	Accounts map[string]struct {
		Account      string `json:"account"`
		FishingLevel *int   `json:"fishingLevel"`
	} `json:"accounts"`
}

func savedFishingLevels() map[string]int {
	levels := make(map[string]int)
	paths := []string{filepath.Join("data", "fishing-reports.json")}
	if executable, err := os.Executable(); err == nil {
		// O executável do motor fica em engine/, enquanto os relatórios ficam
		// na raiz do pacote. Isso mantém a leitura correta mesmo se o atalho
		// iniciar o programa com outro diretório de trabalho.
		paths = append(paths, filepath.Join(filepath.Dir(executable), "..", "data", "fishing-reports.json"))
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var snapshot fishingReportLevelSnapshot
		if json.Unmarshal(contents, &snapshot) != nil {
			continue
		}
		for _, account := range snapshot.Accounts {
			name := strings.ToLower(strings.TrimSpace(account.Account))
			if name != "" && account.FishingLevel != nil && *account.FishingLevel > 0 {
				levels[name] = *account.FishingLevel
			}
		}
		if len(levels) > 0 {
			return levels
		}
	}
	return levels
}

// fishingLevelFromSession prioriza a leitura emitida pela sessão que está
// conectada agora. Assim a distribuição não depende apenas do arquivo do
// painel, que pode estar vazio após um reset ou ainda não ter sido salvo.
func fishingLevelFromSession(events []string) int {
	const marker = "FISHING_STATS nivel="
	for index := len(events) - 1; index >= 0; index-- {
		position := strings.Index(events[index], marker)
		if position < 0 {
			continue
		}
		rest := events[index][position+len(marker):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		level, err := strconv.Atoi(rest[:end])
		if err == nil && level > 0 && level <= 100 {
			return level
		}
	}
	return 0
}

func fishingLevelForSession(item session, persisted map[string]int) (int, string) {
	if level := fishingLevelFromSession(item.Events); level > 0 {
		return level, "sessão atual"
	}
	if level := persisted[strings.ToLower(strings.TrimSpace(item.Nickname))]; level > 0 {
		return level, "histórico salvo"
	}
	return 0, "não informado"
}

// startFishingByLevel distribui cada conta pelo quarto compatível com o último
// nível que o relatório confirmou. A decisão é individual, mas cada comando
// continua usando o mesmo canal de controle já validado pelo motor.
func startFishingByLevel(store *sessionStore, ids []string) ([]string, map[string]string, map[string]int, int) {
	levels := savedFishingLevels()
	started := make([]string, 0, len(ids))
	failures := make(map[string]string)
	distribution := map[string]int{"infobus": 0, "jardim-flutuante": 0, "snouthill-pier": 0}
	unknownLevel := 0

	for _, id := range ids {
		store.mu.RLock()
		item, exists := store.sessions[id]
		store.mu.RUnlock()
		if !exists {
			failures[id] = "conta não encontrada"
			continue
		}
		level, source := fishingLevelForSession(item, levels)
		if level == 0 {
			unknownLevel++
		}
		destination := fishingDestinationForLevel(level)
		fmt.Printf("MARCO_OK: FISHING_BY_LEVEL conta=%s nivel=%d origem=%s destino=%s\n", item.Nickname, level, source, destination)
		currentStarted, currentFailures := commandSessions(store, []string{id}, "fishing:start:"+destination, "iniciando-pesca", destination)
		started = append(started, currentStarted...)
		if len(currentStarted) > 0 {
			distribution[destination]++
		}
		for sessionID, reason := range currentFailures {
			failures[sessionID] = reason
		}
	}
	return started, failures, distribution, unknownLevel
}
