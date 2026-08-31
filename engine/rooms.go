package main

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
)

type roomCatalogItem struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	Access      string `json:"access"`
	Users       int    `json:"users"`
	Capacity    int    `json:"capacity"`
	Description string `json:"description"`
	Port        int    `json:"port,omitempty"`
	Door        int    `json:"door,omitempty"`
}

type roomCatalogResponse struct {
	Rooms      []roomCatalogItem `json:"rooms"`
	OwnerQuery string            `json:"ownerQuery,omitempty"`
	UpdatedAt  *time.Time        `json:"updatedAt,omitempty"`
	Refreshing bool              `json:"refreshing"`
}

type roomOwnerSearchInput struct {
	Owner string `json:"owner"`
}

func currentRoomCatalog(store *sessionStore) roomCatalogResponse {
	store.mu.RLock()
	defer store.mu.RUnlock()
	rooms := make([]roomCatalogItem, 0, len(store.rooms))
	for _, room := range store.rooms {
		rooms = append(rooms, room)
	}
	sort.SliceStable(rooms, func(i, j int) bool {
		if rooms[i].Users == rooms[j].Users {
			return rooms[i].Name < rooms[j].Name
		}
		return rooms[i].Users > rooms[j].Users
	})
	response := roomCatalogResponse{Rooms: rooms, OwnerQuery: store.roomsOwnerQuery, Refreshing: store.roomsRefreshing}
	if !store.roomsUpdatedAt.IsZero() {
		updated := store.roomsUpdatedAt
		response.UpdatedAt = &updated
	}
	return response
}

func requestRoomCatalogRefresh(store *sessionStore) error {
	return requestRoomCatalogCommand(store, "rooms:refresh", "")
}

func requestRoomCatalogOwnerSearch(store *sessionStore, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("informe o nome do Habbo dono do quarto")
	}
	if len([]rune(owner)) > 64 || strings.ContainsAny(owner, "\r\n\x00") {
		return fmt.Errorf("nome de Habbo inválido")
	}
	command := "rooms:owner:" + base64.RawStdEncoding.EncodeToString([]byte(owner))
	return requestRoomCatalogCommand(store, command, owner)
}

func requestRoomCatalogCommand(store *sessionStore, command, ownerQuery string) error {
	store.mu.RLock()
	var selected session
	found := false
	// Uma conta ociosa continua sendo a melhor opção, mas o navegador público
	// também pode ser consultado por uma sessão que já está pescando. O loop de
	// pesca trata FLAT_RESULTS sem liberar o alvo ou interromper a captura.
	for _, item := range store.sessions {
		if item.Automation == "" && item.Status == "conectada" && item.control != nil && item.controlMu != nil {
			selected = item
			found = true
			break
		}
	}
	if !found {
		for _, item := range store.sessions {
			if item.AutomationID == "pesca" && item.Automation != "parando" && item.control != nil && item.controlMu != nil &&
				item.Status != "desconectada" && item.Status != "encerrada" {
				selected = item
				found = true
				break
			}
		}
	}
	store.mu.RUnlock()
	if !found {
		return fmt.Errorf("é necessária ao menos uma conta conectada para consultar os quartos")
	}
	store.mu.Lock()
	store.roomsRefreshing = true
	store.roomsOwnerQuery = ownerQuery
	store.mu.Unlock()
	selected.controlMu.Lock()
	_, err := fmt.Fprintln(selected.control, command)
	selected.controlMu.Unlock()
	if err != nil {
		store.mu.Lock()
		store.roomsRefreshing = false
		store.mu.Unlock()
		return fmt.Errorf("canal da conta indisponível")
	}
	return nil
}

func stopBotSessionsSequentially(store *sessionStore, ids []string, botID string, timeout time.Duration) ([]string, map[string]string) {
	stopped := make([]string, 0, len(ids))
	failures := make(map[string]string)
	for _, id := range ids {
		store.mu.RLock()
		item, ok := store.sessions[id]
		store.mu.RUnlock()
		if !ok || item.AutomationID != botID {
			failures[id] = "esta automação não está ativa na conta"
			continue
		}
		requested, rejected := commandSessions(store, []string{id}, "automation:stop", "parando", "")
		if reason, failed := rejected[id]; failed {
			failures[id] = reason
			continue
		}
		confirmed, unconfirmed := waitForAutomationStop(store, requested, timeout)
		if reason, failed := unconfirmed[id]; failed {
			failures[id] = reason
			continue
		}
		stopped = append(stopped, confirmed...)
	}
	return stopped, failures
}
