package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type session struct {
	ID                  string     `json:"id"`
	ProfileID           string     `json:"profileId,omitempty"`
	Nickname            string     `json:"nickname"`
	Hotel               string     `json:"hotel"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"createdAt"`
	LoginMode           string     `json:"loginMode"`
	Transport           string     `json:"transport"`
	Automation          string     `json:"automation"`
	AutomationID        string     `json:"automationId,omitempty"`
	AutomationTarget    string     `json:"automationTarget,omitempty"`
	AutomationStartedAt *time.Time `json:"automationStartedAt,omitempty"`
	AutomationSeconds   int64      `json:"automationSeconds"`
	AutomationStarts    int        `json:"automationStarts"`
	FishingDestination  string     `json:"fishingDestination,omitempty"`
	FishingRoom         string     `json:"fishingRoom,omitempty"`
	LastEvent           string     `json:"lastEvent,omitempty"`
	DisconnectedReason  string     `json:"disconnectedReason,omitempty"`
	Events              []string   `json:"events,omitempty"`
	AuthorizationReady  bool       `json:"authorizationReady,omitempty"`
	ReconnectAttempts   int        `json:"reconnectAttempts,omitempty"`
	authorizationURL    string
	process             *exec.Cmd
	gearthProcess       *exec.Cmd
	extensionProcess    *exec.Cmd
	extensionPort       int
	control             io.WriteCloser
	controlMu           *sync.Mutex
}

type loginInput struct {
	Nickname  string `json:"nickname"`
	Mode      string `json:"mode"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	TOTP      string `json:"totp"`
	SaveLogin bool   `json:"saveLogin"`
	ProfileID string `json:"profileId"`
}

type savedProfile struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Mode     string `json:"mode"`
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}
type publicProfile struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Mode     string `json:"mode"`
	Email    string `json:"email"`
}

type sessionStore struct {
	mu              sync.RWMutex
	sessions        map[string]session
	rooms           map[int]roomCatalogItem
	roomsUpdatedAt  time.Time
	roomsRefreshing bool
	roomsOwnerQuery string
}

type bot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	Executable  string `json:"-"`
	process     *exec.Cmd
}

type botStore struct {
	mu    sync.RWMutex
	items map[string]bot
}

type automationInput struct {
	SessionIDs  []string `json:"sessionIds"`
	Destination string   `json:"destination"`
	Mode        string   `json:"mode"`
	RoomID      int      `json:"roomId"`
}

type formationShapeInput struct {
	SessionIDs []string `json:"sessionIds"`
	Shape      string   `json:"shape"`
}

type globalChatInput struct {
	Message string `json:"message"`
}

var profilesMu sync.Mutex

var formationShapeNames = map[string]string{
	"coracao": "Coração grande",
	"estrela": "Estrela de cinco pontas",
	"coroa":   "Coroa",
	"peixe":   "Peixe",
	"trofeu":  "Troféu",
	"smile":   "Carinha sorrindo",
	"help":    "HELP",
}

func main() {
	// Exportação usada somente para montar o pacote do servidor. Ela lê as
	// credenciais DPAPI desta máquina e cria uma cópia portátil cifrada, sem
	// imprimir nenhuma senha no console ou nos registros.
	if len(os.Args) == 5 && os.Args[1] == "--export-portable-profiles" {
		if err := exportPortableProfiles(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			log.Printf("não foi possível exportar os logins portáteis: %v", err)
			os.Exit(1)
		}
		return
	}

	// Se o motor foi fechado à força, processos Java do Codex G-Earth podem
	// sobreviver sem uma sessão que os controle. Antes de aceitar novas contas,
	// removemos apenas esses processos órfãos deste próprio aplicativo para que
	// conexões antigas não disputem portas e sessões do hotel.
	cleanupStaleCodexGEarth()
	store := &sessionStore{sessions: make(map[string]session), rooms: make(map[int]roomCatalogItem)}
	bots := newBotStore()
	fishing := newFishingCoordinator()
	mux := http.NewServeMux()
	profilesPath := filepath.Join("data", "saved-logins.json")

	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, _ *http.Request) {
		profiles, _ := loadProfiles(profilesPath)
		items := make([]publicProfile, 0, len(profiles))
		for _, p := range profiles {
			items = append(items, publicProfile{p.ID, p.Nickname, p.Mode, p.Email})
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("DELETE /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		profiles, _ := loadProfiles(profilesPath)
		next := profiles[:0]
		for _, p := range profiles {
			if p.ID != r.PathValue("id") {
				next = append(next, p)
			}
		}
		if err := saveProfiles(profilesPath, next); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"engine":   "habbo-headless",
			"protocol": "g-earth-origins-headless",
		})
	})
	// Endpoints privados do próprio motor. Cada processo de handshake consulta
	// estes pontos somente ao escolher uma sala ou um alvo de pesca; assim as
	// contas deste painel não disputam o mesmo peixe por acidente.
	mux.HandleFunc("POST /api/fishing/lease", func(w http.ResponseWriter, r *http.Request) {
		var input fishingLeaseRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reserva de alvo inválida"})
			return
		}
		decision := fishing.reserve(input)
		writeJSON(w, http.StatusOK, decision)
	})
	mux.HandleFunc("POST /api/fishing/release", func(w http.ResponseWriter, r *http.Request) {
		var input fishingReleaseRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "liberação de alvo inválida"})
			return
		}
		fishing.release(input)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/fishing/room-claim", func(w http.ResponseWriter, r *http.Request) {
		var input fishingRoomClaimRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "seleção de sala inválida"})
			return
		}
		decision := fishing.chooseRoom(input)
		if decision.RoomKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nenhuma sala de pesca elegível"})
			return
		}
		writeJSON(w, http.StatusOK, decision)
	})
	mux.HandleFunc("POST /api/fishing/room-release", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "liberação de sala inválida"})
			return
		}
		fishing.releaseRoom(input.SessionID)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/gearth", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, currentGEarthStatus(store))
	})
	mux.HandleFunc("POST /api/gearth/start", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, currentGEarthStatus(store))
	})
	mux.HandleFunc("GET /api/rooms", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, currentRoomCatalog(store))
	})
	mux.HandleFunc("POST /api/rooms/refresh", func(w http.ResponseWriter, _ *http.Request) {
		if err := requestRoomCatalogRefresh(store); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"refreshing": true})
	})
	mux.HandleFunc("POST /api/rooms/search-owner", func(w http.ResponseWriter, r *http.Request) {
		var input roomOwnerSearchInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nome do Habbo inválido"})
			return
		}
		if err := requestRoomCatalogOwnerSearch(store, input.Owner); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"refreshing": true})
	})
	mux.HandleFunc("GET /api/appearance", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, loadAppearanceState())
	})
	mux.HandleFunc("POST /api/appearance/apply", func(w http.ResponseWriter, r *http.Request) {
		var input appearanceApplyInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "visual inválido"})
			return
		}
		preset, ok := findAppearancePreset(input.PresetID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual não encontrado"})
			return
		}
		if err := saveSelectedAppearance(preset.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		store.mu.RLock()
		ports := make([]int, 0, len(store.sessions))
		for _, item := range store.sessions {
			if item.extensionPort > 0 && item.Status != "desconectada" && item.Status != "encerrada" && processRunning(item.gearthProcess) {
				ports = append(ports, item.extensionPort)
			}
		}
		store.mu.RUnlock()
		applied, failures := applyAppearanceToPorts(ports, preset.Figure)
		writeJSON(w, http.StatusOK, map[string]any{
			"selectedId": preset.ID,
			"applied":    applied,
			"online":     len(ports),
			"failures":   failures,
		})
	})
	mux.HandleFunc("POST /api/chat/global", func(w http.ResponseWriter, r *http.Request) {
		var input globalChatInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mensagem inválida"})
			return
		}
		sent, skipped, failures, err := broadcastGlobalChat(store, input.Message)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		status := http.StatusOK
		payload := map[string]any{"enviadas": sent, "ignoradas": skipped, "falhas": failures}
		if len(sent) == 0 {
			status = http.StatusConflict
			payload["error"] = "nenhuma conta está dentro de um quarto"
		}
		writeJSON(w, status, payload)
	})

	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		store.mu.RLock()
		defer store.mu.RUnlock()
		items := make([]session, 0, len(store.sessions))
		for _, item := range store.sessions {
			if item.AutomationStartedAt != nil {
				item.AutomationSeconds += int64(time.Since(*item.AutomationStartedAt).Seconds())
			}
			// O dashboard não usa o fluxo de eventos. O coletor solicita-o
			// explicitamente, evitando transferir centenas de mensagens por
			// segundo para o navegador.
			if r.URL.Query().Get("includeEvents") != "1" {
				item.Events = nil
			}
			items = append(items, item)
		}
		// sessionStore usa map para acesso concorrente por ID. Como a ordem de
		// iteração de maps em Go é intencionalmente indefinida, ordenar aqui evita
		// que as contas troquem de posição a cada atualização do dashboard.
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		})
		writeJSON(w, http.StatusOK, items)
	})

	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var input loginInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dados de login inválidos"})
			return
		}
		if input.ProfileID != "" {
			profiles, _ := loadProfiles(profilesPath)
			found := false
			for _, p := range profiles {
				if p.ID == input.ProfileID {
					input.Nickname = p.Nickname
					input.Mode = p.Mode
					input.Email = p.Email
					input.Password, _ = unprotect(p.Password)
					if p.TOTP != "" {
						input.TOTP, _ = unprotect(p.TOTP)
					}
					found = true
					break
				}
			}
			if !found {
				writeJSON(w, 404, map[string]string{"error": "login salvo não encontrado"})
				return
			}
		}
		if strings.TrimSpace(input.Nickname) == "" {
			input.Nickname = "Identificando conta…"
		}
		if input.Mode == "" {
			input.Mode = "steam"
		}
		if input.Mode != "steam" && input.Mode != "habbo" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "modo de login inválido"})
			return
		}
		if input.Mode == "habbo" && (input.Email == "" || input.Password == "") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "e-mail e senha são obrigatórios"})
			return
		}
		onAuthenticated, err := authenticatedProfileCallback(profilesPath, input)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item := session{
			ID: time.Now().UTC().Format("20060102T150405.000000000"), ProfileID: input.ProfileID, Nickname: input.Nickname,
			Hotel: "origins.habbo.com.br", Status: "iniciando",
			CreatedAt: time.Now().UTC(), LoginMode: input.Mode, Transport: "g-earth",
		}
		if item.ProfileID == "" && input.Mode == "habbo" && input.SaveLogin {
			item.ProfileID = savedProfileID(input.Email)
		}
		// Registra a sessão antes de iniciar o processo para que nenhum marco
		// inicial do handshake se perca por corrida com o leitor de stdout.
		store.mu.Lock()
		store.sessions[item.ID] = item
		store.mu.Unlock()
		cmd, gearthProcess, extensionProcess, control, err := startHeadless(store, item.ID, input, onAuthenticated)
		input.Password = ""
		input.TOTP = ""
		if err != nil {
			store.mu.Lock()
			delete(store.sessions, item.ID)
			store.mu.Unlock()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item.process = cmd
		item.gearthProcess = gearthProcess
		item.extensionProcess = extensionProcess
		item.control = control
		item.controlMu = &sync.Mutex{}
		store.mu.Lock()
		current := store.sessions[item.ID]
		current.process = cmd
		current.gearthProcess = gearthProcess
		current.extensionProcess = extensionProcess
		current.control = control
		current.controlMu = item.controlMu
		store.sessions[item.ID] = current
		item = current
		store.mu.Unlock()
		writeJSON(w, http.StatusCreated, item)
	})

	mux.HandleFunc("DELETE /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		store.mu.Lock()
		item, ok := store.sessions[id]
		if ok {
			delete(store.sessions, id)
		}
		store.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "sessão não encontrada"})
			return
		}
		if item.process != nil && item.process.Process != nil {
			if item.control != nil {
				_ = item.control.Close()
			}
			_ = item.process.Process.Kill()
		}
		stopProcess(item.extensionProcess)
		stopProcess(item.gearthProcess)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/sessions/{id}/authorize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			store.mu.RLock()
			item, ok := store.sessions[r.PathValue("id")]
			store.mu.RUnlock()
			if !ok {
				http.Error(w, "sessão não encontrada", http.StatusNotFound)
				return
			}
			if item.authorizationURL != "" {
				if err := validateSteamAuthorizationURL(item.authorizationURL); err != nil {
					http.Error(w, "endereço de autorização inválido", http.StatusBadGateway)
					return
				}
				http.Redirect(w, r, item.authorizationURL, http.StatusFound)
				return
			}
			if item.Status == "desconectada" {
				http.Error(w, "a sessão foi desconectada antes da autorização", http.StatusConflict)
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-deadline.C:
				http.Error(w, "o servidor não entregou o endereço de autorização a tempo", http.StatusGatewayTimeout)
				return
			case <-ticker.C:
			}
		}
	})

	mux.HandleFunc("GET /api/bots", func(w http.ResponseWriter, _ *http.Request) {
		bots.mu.RLock()
		defer bots.mu.RUnlock()
		items := make([]bot, 0, len(bots.items))
		for _, item := range bots.items {
			items = append(items, item)
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		writeJSON(w, http.StatusOK, items)
	})

	mux.HandleFunc("POST /api/bots/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		bots.mu.RLock()
		item, ok := bots.items[id]
		if !ok {
			bots.mu.RUnlock()
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bot não encontrado"})
			return
		}
		bots.mu.RUnlock()
		var input automationInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.SessionIDs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione ao menos uma conta"})
			return
		}
		var command, automation, target string
		switch id {
		case "pesca":
			mode, valid := normalizeFishingMode(input.Mode)
			if !valid {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "modo de pesca inválido"})
				return
			}
			if mode == fishingModeByLevel {
				started, failures, distribution, unknownLevel := startFishingByLevel(store, input.SessionIDs)
				if len(started) == 0 {
					writeJSON(w, http.StatusConflict, map[string]any{"error": "nenhuma conta conectada pôde iniciar", "falhas": failures})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"bot":          item.ID,
					"iniciadas":    started,
					"falhas":       failures,
					"modo":         mode,
					"distribuicao": distribution,
					"semNivel":     unknownLevel,
				})
				return
			}
			destination, valid := normalizeFishingDestination(input.Destination)
			if !valid {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "destino de pesca inválido"})
				return
			}
			command, automation, target = "fishing:start:"+destination, "iniciando-pesca", destination
		case "festa":
			if input.RoomID <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione um quarto"})
				return
			}
			store.mu.RLock()
			room, exists := store.rooms[input.RoomID]
			store.mu.RUnlock()
			if !exists {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "atualize e selecione um quarto da lista"})
				return
			}
			if room.Access != "open" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "o quarto selecionado não está aberto"})
				return
			}
			command, automation, target = fmt.Sprintf("party:start:%d:%d:%d", room.ID, room.Port, room.Door), "iniciando-festa", room.Name
		case "formacao":
			if len(input.SessionIDs) != 35 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione exatamente 35 contas para uma formação"})
				return
			}
			if input.RoomID <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione um quarto"})
				return
			}
			store.mu.RLock()
			room, exists := store.rooms[input.RoomID]
			store.mu.RUnlock()
			if !exists {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "atualize e selecione um quarto da lista"})
				return
			}
			if room.Access != "open" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "o quarto selecionado não está aberto"})
				return
			}
			started, failures := startFormationSessions(store, input.SessionIDs, room)
			if len(started) == 0 {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "nenhuma conta conectada pôde iniciar a formação", "falhas": failures})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"bot": item.ID, "iniciadas": started, "falhas": failures})
			return
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "automação não encontrada"})
			return
		}
		started, failures := commandSessions(store, input.SessionIDs, command, automation, target)
		if len(started) == 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "nenhuma conta conectada pôde iniciar", "falhas": failures})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"bot": item.ID, "iniciadas": started, "falhas": failures})
	})

	mux.HandleFunc("POST /api/bots/formacao/shape", func(w http.ResponseWriter, r *http.Request) {
		var input formationShapeInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.SessionIDs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione ao menos uma conta da formação"})
			return
		}
		if _, ok := formationShapeNames[input.Shape]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "figura inválida"})
			return
		}
		applied, failures := commandFormationShape(store, input.SessionIDs, input.Shape)
		if len(applied) == 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "nenhuma conta está em formação", "falhas": failures})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"aplicadas": applied, "falhas": failures})
	})

	mux.HandleFunc("POST /api/bots/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		bots.mu.RLock()
		item, ok := bots.items[id]
		if !ok {
			bots.mu.RUnlock()
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bot não encontrado"})
			return
		}
		bots.mu.RUnlock()
		var input automationInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.SessionIDs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione ao menos uma conta"})
			return
		}
		stopped, failures := stopBotSessionsSequentially(store, input.SessionIDs, id, 12*time.Second)
		status := http.StatusOK
		payload := map[string]any{"bot": item.ID, "paradas": stopped, "falhas": failures}
		if len(stopped) == 0 {
			status = http.StatusConflict
			payload["error"] = "nenhuma conta confirmou a parada"
		}
		writeJSON(w, status, payload)
	})

	address := "127.0.0.1:8787"
	if value := os.Getenv("HABBO_HEADLESS_ADDR"); value != "" {
		address = value
	}
	log.Printf("motor local ouvindo em http://%s", address)
	log.Fatal(http.ListenAndServe(address, withLocalCORS(mux)))
}

func cleanupStaleCodexGEarth() {
	root := strings.ReplaceAll(codexGEarthRoot(), "'", "''")
	script := fmt.Sprintf("$ErrorActionPreference='SilentlyContinue'; Get-CimInstance Win32_Process -Filter \"Name='java.exe'\" | Where-Object { $_.ProcessId -ne %d -and $_.CommandLine -like '*%s*' -and $_.CommandLine -like '*CodexGEarthMain*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force }", os.Getpid(), root)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = windowsPowerShellEnv()
	if err := cmd.Run(); err != nil {
		log.Printf("limpeza preventiva do Codex G-Earth não concluída: %v", err)
	}
}

func newBotStore() *botStore {
	definitions := []bot{
		{ID: "pesca", Name: "Pesca rápida", Description: "Conexão pelo Codex G-Earth com pesca executada pelo motor headless otimizado.", Mode: "Codex G-Earth + motor nativo"},
		{ID: "festa", Name: "Passeio e festa", Description: "Leva as contas a um quarto de jogador, dança e circula continuamente pelo ambiente.", Mode: "Navegador de quartos + motor nativo"},
		{ID: "formacao", Name: "Formações", Description: "Organiza exatamente 35 contas em uma fila e depois em uma figura ou palavra no quarto escolhido.", Mode: "Navegador de quartos + posições coordenadas"},
	}
	items := make(map[string]bot, len(definitions))
	for _, item := range definitions {
		item.Status = "parado"
		items[item.ID] = item
	}
	return &botStore{items: items}
}

func startHeadless(store *sessionStore, id string, login loginInput, onAuthenticated func(string)) (*exec.Cmd, *exec.Cmd, *exec.Cmd, io.WriteCloser, error) {
	// Contas Habbo que já chegaram ao LOGIN_OK podem ser reconectadas sem pedir
	// novamente a senha ao usuário. A credencial continua somente na memória do
	// processo e a cópia persistida permanece protegida pelo DPAPI do Windows.
	reconnectLogin := login
	if reconnectLogin.Mode == "habbo" && reconnectLogin.ProfileID == "" {
		reconnectLogin.ProfileID = savedProfileID(reconnectLogin.Email)
	}
	executable := handshakeExecutable()
	if _, err := os.Stat(executable); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("motor de protocolo não compilado: %w", err)
	}
	authentication := map[string]string{"email": login.Email, "password": login.Password, "totp": login.TOTP}
	proxyPort, err := freeLocalPort()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("não foi possível reservar a porta do proxy: %w", err)
	}
	requestedExtensionPort, err := freeLocalPort()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("não foi possível reservar a porta da extensão: %w", err)
	}
	targetHost, err := resolvePublicOriginsHost()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	gearthProcess, extensionPort, err := launchCodexGEarth(proxyPort, requestedExtensionPort, targetHost)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// A extensão de visual apenas aguarda a identificação da conta, aplica o
	// uniforme padrão e encerra. A pesca continua nativa no handshake.
	store.mu.Lock()
	if item, ok := store.sessions[id]; ok {
		item.extensionPort = extensionPort
		store.sessions[id] = item
	}
	store.mu.Unlock()
	extension, err := launchGEarthSkinExtension(extensionPort, selectedAppearanceFigure())
	if err != nil {
		stopProcess(gearthProcess)
		return nil, nil, nil, nil, err
	}

	cmd := exec.Command(executable, fmt.Sprintf("127.0.0.1:%d", proxyPort))
	coordinatorAddress := os.Getenv("HABBO_HEADLESS_ADDR")
	if coordinatorAddress == "" {
		coordinatorAddress = "127.0.0.1:8787"
	}
	coordinatorAddress = strings.Replace(coordinatorAddress, "0.0.0.0:", "127.0.0.1:", 1)
	cmd.Env = append(
		os.Environ(),
		"HABBO_OPEN_AUTH=0",
		"HABBO_LOGIN_MODE="+login.Mode,
		"HABBO_FISHING_DRIVER=native",
		"HABBO_SESSION_ID="+id,
		"HABBO_FISHING_COORDINATOR_URL=http://"+coordinatorAddress,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stopProcess(extension)
		stopProcess(gearthProcess)
		return nil, nil, nil, nil, err
	}
	if login.Mode == "habbo" {
		authenticationPayload := authentication
		go func(payload map[string]string) {
			_ = json.NewEncoder(stdin).Encode(payload)
		}(authenticationPayload)
	}
	login.Password = ""
	login.TOTP = ""
	authentication = nil
	output, err := cmd.StdoutPipe()
	if err != nil {
		stopProcess(extension)
		stopProcess(gearthProcess)
		return nil, nil, nil, nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		stopProcess(extension)
		stopProcess(gearthProcess)
		return nil, nil, nil, nil, err
	}
	go func() {
		resolvedNickname := login.Nickname
		authenticatedProfileSaved := false
		lastProcessLine := ""
		catalogBuffer := make(map[int]roomCatalogItem)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Text()
			lastProcessLine = line
			if strings.Contains(line, "MARCO_OK: ROOM_CATALOG_BEGIN") {
				catalogBuffer = make(map[int]roomCatalogItem)
				continue
			}
			if marker := "ROOM_CATALOG: "; strings.HasPrefix(line, marker) {
				var room roomCatalogItem
				if json.Unmarshal([]byte(strings.TrimPrefix(line, marker)), &room) == nil && room.ID > 0 {
					catalogBuffer[room.ID] = room
				}
				continue
			}
			if strings.Contains(line, "MARCO_OK: ROOM_CATALOG_DONE") {
				store.mu.Lock()
				store.rooms = catalogBuffer
				store.roomsUpdatedAt = time.Now().UTC()
				store.roomsRefreshing = false
				store.mu.Unlock()
				continue
			}
			if strings.Contains(line, "MARCO_OK: ROOM_CATALOG_ERROR") {
				store.mu.Lock()
				store.roomsRefreshing = false
				store.mu.Unlock()
			}
			if marker := "AUTH_OPENID_URL: "; strings.HasPrefix(line, marker) {
				authorizationURL := strings.TrimSpace(strings.TrimPrefix(line, marker))
				if validateSteamAuthorizationURL(authorizationURL) == nil {
					store.mu.Lock()
					if item, ok := store.sessions[id]; ok {
						item.authorizationURL = authorizationURL
						item.AuthorizationReady = true
						item.Status = "aguardando-autorização"
						store.sessions[id] = item
					}
					store.mu.Unlock()
				}
				continue
			}
			status := ""
			identifiedNickname := ""
			fishingDestination := ""
			fishingRoom := ""
			switch {
			case strings.Contains(line, "SESSION_PARAMETERS"):
				status = "autenticando"
			case strings.Contains(line, "MARCO_OK: LOGIN_OK recebido"):
				status = "conectada"
			case strings.Contains(line, "INFOBUS_ROOM_READY"):
				status = "no-infobus"
			case strings.Contains(line, "FISHING_ROOM_READY"):
				status = "no-quarto-pesca"
			case strings.Contains(line, "FISHING_ROOM_RECOVERY"):
				status = "recuperando-sala"
			case strings.Contains(line, "aguardando área de pesca"):
				status = "aguardando-peixe"
			case strings.Contains(line, "FISHING_STOPPED"):
				status = "conectada"
			case strings.Contains(line, "FORMATION_ROOM_READY"):
				status = "no-quarto-formacao"
			case strings.Contains(line, "FORMATION_QUEUE"):
				status = "formando-fila"
			case strings.Contains(line, "FORMATION_SHAPE"):
				status = "formando-figura"
			case strings.Contains(line, "FORMATION_MOVING"):
				status = "organizando-formacao"
			case strings.Contains(line, "PARTY_ROOM_READY"):
				status = "no-quarto-festa"
			case strings.Contains(line, "PARTY_STARTED"):
				status = "festejando"
			case strings.Contains(line, "PARTY_WALKING"):
				status = "passeando"
			case strings.Contains(line, "PARTY_DANCING"):
				status = "dançando"
			case strings.Contains(line, "EXIT_WALK_STARTED"):
				status = "saindo-do-quarto"
			case strings.Contains(line, "AUTOMATION_STOPPED"):
				status = "conectada"
			case strings.Contains(line, "CAST_SENT"):
				status = "vara-lançada"
			case strings.Contains(line, "MOVENDO_PARA_PEIXE"), strings.Contains(line, "MOVENDO_DIRETO_PARA_PEIXE"), strings.Contains(line, "MOVIMENTO_DIRETO_REENVIADO"), strings.Contains(line, "MOVIMENTO_REPETIDO"), strings.Contains(line, "ALVO_TRAVADO_MOVENDO"), strings.Contains(line, "ALVO_TRAVADO_MOVIMENTO_REENVIADO"), strings.Contains(line, "DESTINO_ALTERNATIVO"):
				status = "indo-pescar"
			case strings.Contains(line, "WAVE enviado"):
				status = "visível-no-infobus"
			}
			if marker := "ACCOUNT_IDENTIFIED nome="; strings.Contains(line, marker) {
				identifiedNickname = strings.TrimSpace(line[strings.Index(line, marker)+len(marker):])
				resolvedNickname = identifiedNickname
			}
			if marker := "MARCO_OK: FISHING_ROOM_SELECTED destino="; strings.HasPrefix(line, marker) {
				parts := strings.SplitN(strings.TrimPrefix(line, marker), " nome=", 2)
				fishingDestination = strings.TrimSpace(parts[0])
				if len(parts) == 2 {
					fishingRoom = strings.TrimSpace(parts[1])
				}
			}
			if !authenticatedProfileSaved && strings.Contains(line, "MARCO_OK: LOGIN_OK recebido") {
				authenticatedProfileSaved = true
				if onAuthenticated != nil {
					onAuthenticated(resolvedNickname)
				}
			}
			if status != "" || identifiedNickname != "" || fishingDestination != "" || fishingRoom != "" {
				store.mu.Lock()
				if item, ok := store.sessions[id]; ok {
					if status != "" {
						item.Status = status
					}
					if identifiedNickname != "" {
						item.Nickname = identifiedNickname
					}
					if fishingDestination != "" {
						item.FishingDestination = fishingDestination
					}
					if fishingRoom != "" {
						item.FishingRoom = fishingRoom
					}
					if status == "conectada" {
						item.authorizationURL = ""
						item.AuthorizationReady = false
						item.ReconnectAttempts = 0
						item.DisconnectedReason = ""
					}
					if status == "vara-lançada" {
						item.Automation = "pescando"
					}
					if status == "festejando" || status == "passeando" || status == "dançando" {
						item.Automation = "festa"
					}
					if strings.Contains(line, "FISHING_STOPPED") || strings.Contains(line, "PARTY_STOPPED") || strings.Contains(line, "AUTOMATION_STOPPED") {
						finishAutomation(&item, time.Now().UTC())
					}
					store.sessions[id] = item
				}
				store.mu.Unlock()
			}
			lowerLine := strings.ToLower(line)
			if strings.Contains(line, "MARCO_OK") || strings.Contains(line, "TRACE_LOGIN") || strings.Contains(line, "ignorado") || strings.Contains(lowerLine, "erro") || strings.Contains(lowerLine, "fatal") || strings.Contains(lowerLine, "não foi") || strings.Contains(lowerLine, "ausente") || strings.Contains(lowerLine, "encerr") {
				store.mu.Lock()
				if item, ok := store.sessions[id]; ok {
					item.LastEvent = line
					item.Events = append(item.Events, line)
					if len(item.Events) > 120 {
						item.Events = item.Events[len(item.Events)-120:]
					}
					store.sessions[id] = item
				}
				store.mu.Unlock()
			}
		}
		scanErr := scanner.Err()
		waitErr := cmd.Wait()
		stopProcess(extension)
		stopProcess(gearthProcess)
		disconnectReason := "o processo de conexão encerrou sem informar a causa"
		switch {
		case scanErr != nil:
			disconnectReason = "falha ao ler o motor de protocolo: " + scanErr.Error()
		case waitErr != nil && lastProcessLine != "":
			disconnectReason = lastProcessLine + " (processo: " + waitErr.Error() + ")"
		case waitErr != nil:
			disconnectReason = "motor de protocolo encerrado: " + waitErr.Error()
		case lastProcessLine != "":
			disconnectReason = lastProcessLine
		}
		log.Printf("[sessão %s/%s] desconectada: %s", id, resolvedNickname, disconnectReason)
		shouldReconnect := false
		store.mu.Lock()
		if item, ok := store.sessions[id]; ok && item.Status != "encerrada" {
			verificationNeeded := verificationRequired(disconnectReason)
			if verificationNeeded {
				// Não insistimos em reconectar enquanto o hotel aguarda uma
				// confirmação do titular. Isso evita tentativas em loop e deixa a
				// conta claramente identificada no painel.
				item.Status = "aguardando-verificacao"
			} else {
				item.Status = "desconectada"
			}
			item.LastEvent = disconnectReason
			item.DisconnectedReason = disconnectReason
			item.Events = append(item.Events, "DESCONEXÃO: "+disconnectReason)
			if len(item.Events) > 120 {
				item.Events = item.Events[len(item.Events)-120:]
			}
			item.Automation = ""
			if item.AutomationStartedAt != nil {
				item.AutomationSeconds += int64(time.Since(*item.AutomationStartedAt).Seconds())
				item.AutomationStartedAt = nil
			}
			shouldReconnect = !verificationNeeded && authenticatedProfileSaved && reconnectLogin.Mode == "habbo" && reconnectLogin.ProfileID != ""
			store.sessions[id] = item
		}
		store.mu.Unlock()
		if shouldReconnect {
			go reconnectHeadless(store, id, reconnectLogin, onAuthenticated)
		}
	}()
	return cmd, gearthProcess, extension, stdin, nil
}

// handshakeExecutable permite validar uma revisão nova lado a lado sem trocar
// o binário que mantém as contas atuais conectadas. O launcher da revisão usa
// o mesmo sufixo e, quando ela for aberta depois da parada, todos os processos
// do conjunto passam a usar a mesma versão.
func handshakeExecutable() string {
	standard := filepath.Join("bin", "habbo-handshake.exe")
	if !runningPerformanceV2() {
		return standard
	}
	versioned := filepath.Join("bin", "habbo-handshake.performance-v2.exe")
	if info, err := os.Stat(versioned); err == nil && !info.IsDir() {
		return versioned
	}
	return standard
}

func runningPerformanceV2() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(filepath.Base(executable)), "performance-v2")
}

func verificationRequired(reason string) bool {
	reason = strings.ToLower(reason)
	return strings.Contains(reason, "código de uso único") ||
		strings.Contains(reason, "codigo de uso unico") ||
		strings.Contains(reason, "verificação de dois fatores") ||
		strings.Contains(reason, "verificacao de dois fatores")
}

func reconnectHeadless(store *sessionStore, id string, login loginInput, onAuthenticated func(string)) {
	for {
		store.mu.Lock()
		item, ok := store.sessions[id]
		if !ok || item.Status == "encerrada" {
			store.mu.Unlock()
			return
		}
		item.ReconnectAttempts++
		attempt := item.ReconnectAttempts
		item.Status = "reconectando"
		item.LastEvent = fmt.Sprintf("reconexão automática %d agendada", attempt)
		store.sessions[id] = item
		store.mu.Unlock()

		delay := time.Duration(attempt*2) * time.Second
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		time.Sleep(delay)

		store.mu.RLock()
		_, stillExists := store.sessions[id]
		store.mu.RUnlock()
		if !stillExists {
			return
		}
		cmd, gearthProcess, extensionProcess, control, err := startHeadless(store, id, login, onAuthenticated)
		if err != nil {
			log.Printf("[sessão %s] tentativa de reconexão %d falhou: %v", id, attempt, err)
			store.mu.Lock()
			if current, exists := store.sessions[id]; exists {
				current.Status = "desconectada"
				current.LastEvent = "reconexão falhou: " + err.Error()
				current.DisconnectedReason = current.LastEvent
				store.sessions[id] = current
			}
			store.mu.Unlock()
			continue
		}
		store.mu.Lock()
		if current, exists := store.sessions[id]; exists {
			current.process = cmd
			current.gearthProcess = gearthProcess
			current.extensionProcess = extensionProcess
			current.control = control
			current.controlMu = &sync.Mutex{}
			current.Status = "iniciando"
			store.sessions[id] = current
		}
		store.mu.Unlock()
		return
	}
}

func stopProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func validateSteamAuthorizationURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("URL de autorização inválida")
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := host == "steamcommunity.com" || strings.HasSuffix(host, ".steamcommunity.com") ||
		host == "habbo.com" || strings.HasSuffix(host, ".habbo.com") ||
		host == "habbo.com.br" || strings.HasSuffix(host, ".habbo.com.br")
	if !allowed {
		return fmt.Errorf("host de autorização não permitido: %s", host)
	}
	return nil
}

func commandSessions(store *sessionStore, ids []string, command, automation, destination string) ([]string, map[string]string) {
	started := []string{}
	failures := map[string]string{}
	for _, id := range ids {
		store.mu.RLock()
		item, ok := store.sessions[id]
		store.mu.RUnlock()
		if !ok || item.control == nil || item.controlMu == nil || item.Status == "desconectada" || item.Status == "encerrada" {
			failures[id] = "conta não conectada"
			continue
		}
		startingFishing := strings.HasPrefix(command, "fishing:start:")
		startingParty := strings.HasPrefix(command, "party:start:")
		startingFormation := strings.HasPrefix(command, "formation:start:")
		starting := startingFishing || startingParty || startingFormation
		if starting && !sessionReadyForAutomation(item.Status) {
			failures[id] = "a conta ainda não terminou de conectar"
			continue
		}
		if starting && item.Automation != "" {
			failures[id] = "já existe uma automação ativa nesta conta"
			continue
		}
		if isStopAutomationCommand(command) && item.Automation == "" {
			failures[id] = "não existe automação ativa nesta conta"
			continue
		}
		item.controlMu.Lock()
		_, err := fmt.Fprintln(item.control, command)
		item.controlMu.Unlock()
		if err != nil {
			failures[id] = "canal de controle indisponível"
			continue
		}
		store.mu.Lock()
		current := store.sessions[id]
		if starting {
			now := time.Now().UTC()
			current.AutomationStartedAt = &now
			current.AutomationStarts++
			current.Automation = automation
			current.AutomationTarget = destination
			if startingFishing {
				current.AutomationID = "pesca"
				current.FishingDestination = destination
				current.FishingRoom = ""
			} else if startingFormation {
				current.AutomationID = "formacao"
			} else {
				current.AutomationID = "festa"
			}
		} else {
			// O estado só é encerrado quando o processo responder com
			// FISHING_STOPPED. Isso impede o dashboard de declarar sucesso antes
			// de o loop de pesca realmente parar.
			current.Automation = automation
		}
		store.sessions[id] = current
		store.mu.Unlock()
		started = append(started, id)
	}
	return started, failures
}

func startFormationSessions(store *sessionStore, ids []string, room roomCatalogItem) ([]string, map[string]string) {
	started := make([]string, 0, len(ids))
	failures := make(map[string]string)
	for slot, id := range ids {
		command := fmt.Sprintf("formation:start:%d:%d:%d:%d:%d", room.ID, room.Port, room.Door, slot, len(ids))
		currentStarted, currentFailures := commandSessions(store, []string{id}, command, "formando-fila", room.Name)
		started = append(started, currentStarted...)
		for sessionID, reason := range currentFailures {
			failures[sessionID] = reason
		}
	}
	return started, failures
}

func commandFormationShape(store *sessionStore, ids []string, shape string) ([]string, map[string]string) {
	applied := make([]string, 0, len(ids))
	failures := make(map[string]string)
	command := "formation:shape:" + shape
	for _, id := range ids {
		store.mu.RLock()
		item, ok := store.sessions[id]
		store.mu.RUnlock()
		if !ok || item.AutomationID != "formacao" || item.Automation == "parando" || item.control == nil || item.controlMu == nil {
			failures[id] = "a conta não está em uma formação ativa"
			continue
		}
		item.controlMu.Lock()
		_, err := fmt.Fprintln(item.control, command)
		item.controlMu.Unlock()
		if err != nil {
			failures[id] = "canal de controle indisponível"
			continue
		}
		store.mu.Lock()
		current := store.sessions[id]
		current.Automation = "formando-" + shape
		store.sessions[id] = current
		store.mu.Unlock()
		applied = append(applied, id)
	}
	return applied, failures
}

func sessionReadyForAutomation(status string) bool {
	return status == "conectada" || status == "no-infobus" || status == "visível-no-infobus" ||
		status == "no-quarto-pesca" || status == "aguardando-peixe" || status == "indo-pescar" || status == "vara-lançada"
}

func isStopAutomationCommand(command string) bool {
	return command == "automation:stop" || command == "fishing:stop" || command == "party:stop"
}

const globalChatCommandPrefix = "chat:send:"

func prepareGlobalChatCommand(message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("escreva uma mensagem antes de enviar")
	}
	if strings.ContainsAny(message, "\r\n\x00\x02") {
		return "", fmt.Errorf("a mensagem deve ocupar uma única linha")
	}
	if len([]rune(message)) > 100 {
		return "", fmt.Errorf("a mensagem pode ter no máximo 100 caracteres")
	}
	return globalChatCommandPrefix + base64.RawStdEncoding.EncodeToString([]byte(message)), nil
}

func isSessionInsideRoom(status string) bool {
	switch status {
	case "no-infobus", "visível-no-infobus", "pescando-no-infobus",
		"no-quarto-pesca", "aguardando-peixe", "indo-pescar", "vara-lançada",
		"no-quarto-festa", "festejando", "passeando", "dançando",
		"no-quarto-formacao", "formando-fila", "formando-figura", "organizando-formacao":
		return true
	default:
		return false
	}
}

func broadcastGlobalChat(store *sessionStore, message string) ([]string, int, map[string]string, error) {
	command, err := prepareGlobalChatCommand(message)
	if err != nil {
		return nil, 0, nil, err
	}
	store.mu.RLock()
	sessions := make([]session, 0, len(store.sessions))
	for _, item := range store.sessions {
		sessions = append(sessions, item)
	}
	store.mu.RUnlock()
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	sent := make([]string, 0, len(sessions))
	skipped := 0
	failures := make(map[string]string)
	for _, item := range sessions {
		if !isSessionInsideRoom(item.Status) || item.control == nil || item.controlMu == nil {
			skipped++
			continue
		}
		item.controlMu.Lock()
		_, writeErr := fmt.Fprintln(item.control, command)
		item.controlMu.Unlock()
		if writeErr != nil {
			failures[item.ID] = "canal de controle indisponível"
			continue
		}
		sent = append(sent, item.ID)
	}
	return sent, skipped, failures, nil
}

func normalizeFishingDestination(destination string) (string, bool) {
	destination = strings.ToLower(strings.TrimSpace(destination))
	if destination == "" {
		return "infobus", true
	}
	switch destination {
	case "infobus", "jardim-flutuante", "snouthill-pier":
		return destination, true
	default:
		return "", false
	}
}

func finishAutomation(item *session, stoppedAt time.Time) {
	if item.AutomationStartedAt != nil {
		item.AutomationSeconds += int64(stoppedAt.Sub(*item.AutomationStartedAt).Seconds())
		item.AutomationStartedAt = nil
	}
	item.Automation = ""
	item.AutomationID = ""
	item.AutomationTarget = ""
}

func waitForAutomationStop(store *sessionStore, ids []string, timeout time.Duration) ([]string, map[string]string) {
	pending := make(map[string]bool, len(ids))
	for _, id := range ids {
		pending[id] = true
	}
	deadline := time.Now().Add(timeout)
	for len(pending) > 0 && time.Now().Before(deadline) {
		store.mu.RLock()
		for id := range pending {
			item, ok := store.sessions[id]
			if ok && item.Automation == "" && item.Status == "conectada" {
				delete(pending, id)
			}
		}
		store.mu.RUnlock()
		if len(pending) > 0 {
			time.Sleep(25 * time.Millisecond)
		}
	}
	confirmed := make([]string, 0, len(ids)-len(pending))
	failures := make(map[string]string, len(pending))
	for _, id := range ids {
		if pending[id] {
			failures[id] = "o motor não confirmou a parada dentro do prazo"
		} else {
			confirmed = append(confirmed, id)
		}
	}
	return confirmed, failures
}

func authenticatedProfileCallback(path string, input loginInput) (func(string), error) {
	if input.Mode != "habbo" {
		return nil, nil
	}
	if input.SaveLogin {
		encryptedPassword, err := protect(input.Password)
		if err != nil {
			return nil, fmt.Errorf("não foi possível proteger a senha: %w", err)
		}
		encryptedTOTP, err := protect(input.TOTP)
		if err != nil {
			return nil, fmt.Errorf("não foi possível proteger o 2FA: %w", err)
		}
		pending := savedProfile{
			ID: savedProfileID(input.Email), Nickname: input.Nickname, Mode: "habbo",
			Email: input.Email, Password: encryptedPassword, TOTP: encryptedTOTP,
		}
		return func(resolvedNickname string) {
			if !temporaryNickname(resolvedNickname) {
				pending.Nickname = strings.TrimSpace(resolvedNickname)
			} else if temporaryNickname(pending.Nickname) {
				pending.Nickname = pending.Email
			}
			if err := upsertAuthenticatedProfile(path, pending); err != nil {
				log.Printf("não foi possível salvar o login autenticado %s: %v", pending.Email, err)
			}
		}, nil
	}
	if input.ProfileID != "" {
		profileID := input.ProfileID
		return func(resolvedNickname string) {
			if temporaryNickname(resolvedNickname) {
				return
			}
			if err := updateAuthenticatedProfileName(path, profileID, strings.TrimSpace(resolvedNickname)); err != nil {
				log.Printf("não foi possível atualizar o nome do login %s: %v", profileID, err)
			}
		}, nil
	}
	return nil, nil
}

func savedProfileID(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return fmt.Sprintf("habbo-%x", sum[:8])
}

func temporaryNickname(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || strings.HasPrefix(value, "identificando conta")
}

func upsertAuthenticatedProfile(path string, profile savedProfile) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := loadProfiles(path)
	if err != nil {
		return err
	}
	filtered := make([]savedProfile, 0, len(profiles)+1)
	for _, current := range profiles {
		if current.ID == profile.ID || strings.EqualFold(strings.TrimSpace(current.Email), strings.TrimSpace(profile.Email)) {
			continue
		}
		filtered = append(filtered, current)
	}
	filtered = append(filtered, profile)
	return saveProfiles(path, filtered)
}

func updateAuthenticatedProfileName(path, profileID, nickname string) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := loadProfiles(path)
	if err != nil {
		return err
	}
	changed := false
	for index := range profiles {
		if profiles[index].ID == profileID && profiles[index].Nickname != nickname {
			profiles[index].Nickname = nickname
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveProfiles(path, profiles)
}

func loadProfiles(path string) ([]savedProfile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []savedProfile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []savedProfile
	err = json.Unmarshal(data, &items)
	return items, err
}
func saveProfiles(path string, items []savedProfile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
func protect(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$b=[Console]::In.ReadToEnd(); $v=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b)); ConvertFrom-SecureString (ConvertTo-SecureString $v -AsPlainText -Force)")
	cmd.Env = windowsPowerShellEnv()
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(value)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("DPAPI não protegeu a credencial: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
func unprotect(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, portableCredentialPrefix) {
		return unprotectPortable(value, filepath.Join("data", portableCredentialKeyName))
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$v=[Console]::In.ReadToEnd(); $s=ConvertTo-SecureString $v; $p=(New-Object System.Net.NetworkCredential('', $s)).Password; [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($p))")
	cmd.Env = windowsPowerShellEnv()
	cmd.Stdin = strings.NewReader(value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("DPAPI não abriu a credencial: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	plain, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if decodeErr != nil {
		return "", fmt.Errorf("resposta DPAPI inválida: %w", decodeErr)
	}
	return string(plain), nil
}

func windowsPowerShellEnv() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "PSMODULEPATH=") {
			environment = append(environment, entry)
		}
	}
	return environment
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func withLocalCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://127.0.0.1:3000" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
