package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type appearancePreset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Figure      string `json:"figure"`
	Preview     string `json:"preview"`
}

type appearanceConfig struct {
	SelectedID string `json:"selectedId"`
}

type appearanceState struct {
	SelectedID string             `json:"selectedId"`
	Presets    []appearancePreset `json:"presets"`
}

type appearanceApplyInput struct {
	PresetID string `json:"presetId"`
}

var appearanceMu sync.Mutex

var appearancePresets = []appearancePreset{
	{
		ID:          "pescador-basico",
		Name:        "Pescador básico",
		Description: "Visual simples de pescador com as peças-base do avatar, sem HC, chapéu ou item de catálogo.",
		Figure:      "hd-209-1005.ch-210-1281.lg-285-1281.sh-300-1281.hr-170-1110",
		Preview:     "",
	},
	{
		ID:          "veado",
		Name:        "Veado clássico",
		Description: "Chifres de veado com roupa neutra — o visual mapeado no teste.",
		Figure:      "hd-209-1005.ch-210-1281.lg-285-1281.sh-300-1281.hr-170-1110.ha-1007-1289",
		Preview:     "/skins/veado.png",
	},
	{
		ID:          "reggae",
		Name:        "Reggae",
		Description: "Gorro grande de crochê vermelho, amarelo e verde com roupa preta.",
		Figure:      "hd-209-1005.ch-215-1189.lg-285-1189.sh-300-1189.hr-170-1110.ha-1001-1",
		Preview:     "/skins/reggae.png",
	},
	{
		ID:          "executivo",
		Name:        "Executivo",
		Description: "Terno preto e visual discreto para a skin de negócios.",
		Figure:      "hd-209-1005.ch-255-1189.lg-285-1189.sh-300-1189.hr-170-1110",
		Preview:     "/skins/executivo.png",
	},
	{
		ID:          "codex-preto",
		Name:        "Codex preto",
		Description: "Careca, barba grande e roupa preta.",
		Figure:      "hd-180-1.ch-210-1189.lg-270-1189.sh-290-1189.fa-1205-1189",
		Preview:     "/skins/codex-preto.png",
	},
	{
		ID:          "cowboy",
		Name:        "Cowboy",
		Description: "Chapéu de cowboy e conjunto em tons terrosos.",
		Figure:      "hd-209-1005.ch-215-1281.lg-285-1281.sh-300-1281.hr-170-1110.ha-1013-1",
		Preview:     "/skins/cowboy.png",
	},
	{
		ID:          "realeza",
		Name:        "Realeza",
		Description: "Coroa clássica com terno preto.",
		Figure:      "hd-209-1005.ch-255-1189.lg-285-1189.sh-300-1189.hr-170-1110.ha-1016-1",
		Preview:     "/skins/realeza.png",
	},
}

func appearanceConfigPath() string {
	return filepath.Join("data", "appearance.json")
}

func findAppearancePreset(id string) (appearancePreset, bool) {
	for _, preset := range appearancePresets {
		if preset.ID == id {
			return preset, true
		}
	}
	return appearancePreset{}, false
}

func loadAppearanceState() appearanceState {
	appearanceMu.Lock()
	defer appearanceMu.Unlock()
	selectedID := appearancePresets[0].ID
	if data, err := os.ReadFile(appearanceConfigPath()); err == nil {
		var config appearanceConfig
		if json.Unmarshal(data, &config) == nil {
			if _, ok := findAppearancePreset(config.SelectedID); ok {
				selectedID = config.SelectedID
			}
		}
	}
	return appearanceState{SelectedID: selectedID, Presets: appearancePresets}
}

func saveSelectedAppearance(id string) error {
	if _, ok := findAppearancePreset(id); !ok {
		return fmt.Errorf("visual não encontrado")
	}
	appearanceMu.Lock()
	defer appearanceMu.Unlock()
	path := appearanceConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(appearanceConfig{SelectedID: id}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func selectedAppearanceFigure() string {
	state := loadAppearanceState()
	preset, _ := findAppearancePreset(state.SelectedID)
	return preset.Figure
}

func applyAppearanceToPorts(ports []int, figure string) (int, map[int]string) {
	type result struct {
		port int
		err  error
	}
	results := make(chan result, len(ports))
	for _, port := range ports {
		go func(extensionPort int) {
			cmd, err := launchGEarthSkinExtension(extensionPort, figure)
			if err == nil {
				err = cmd.Wait()
			}
			results <- result{port: extensionPort, err: err}
		}(port)
	}
	applied := 0
	failures := make(map[int]string)
	for range ports {
		result := <-results
		if result.err != nil {
			failures[result.port] = result.err.Error()
			continue
		}
		applied++
	}
	return applied, failures
}

func processRunning(cmd *exec.Cmd) bool {
	return cmd != nil && cmd.Process != nil && cmd.ProcessState == nil
}
