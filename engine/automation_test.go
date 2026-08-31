package main

import (
	"bytes"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessoesPermanecemOrdenadasPorCriacao(t *testing.T) {
	base := time.Now()
	items := []session{
		{ID: "terceira", CreatedAt: base.Add(2 * time.Second)},
		{ID: "primeira", CreatedAt: base},
		{ID: "segunda", CreatedAt: base.Add(time.Second)},
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if items[0].ID != "primeira" || items[1].ID != "segunda" || items[2].ID != "terceira" {
		t.Fatalf("ordem instável: %v", []string{items[0].ID, items[1].ID, items[2].ID})
	}
}

type testWriteCloser struct{ bytes.Buffer }

func (*testWriteCloser) Close() error { return nil }

func TestStopAceitaContaComVaraLancada(t *testing.T) {
	t.Parallel()
	control := &testWriteCloser{}
	startedAt := time.Now().Add(-time.Minute)
	store := &sessionStore{sessions: map[string]session{
		"conta": {
			ID:                  "conta",
			Status:              "vara-lançada",
			Automation:          "pescando",
			AutomationStartedAt: &startedAt,
			control:             control,
			controlMu:           &sync.Mutex{},
		},
	}}

	requested, failures := commandSessions(store, []string{"conta"}, "fishing:stop", "parando", "")
	if len(failures) != 0 || len(requested) != 1 {
		t.Fatalf("parada recusada: solicitadas=%v falhas=%v", requested, failures)
	}
	if !strings.Contains(control.String(), "fishing:stop") {
		t.Fatalf("canal não recebeu fishing:stop: %q", control.String())
	}
	if got := store.sessions["conta"].Automation; got != "parando" {
		t.Fatalf("automação = %q; esperado parando até a confirmação", got)
	}
}

func TestInicioEncaminhaDestinoSelecionado(t *testing.T) {
	t.Parallel()
	control := &testWriteCloser{}
	store := &sessionStore{sessions: map[string]session{
		"conta": {
			ID: "conta", Status: "conectada", control: control, controlMu: &sync.Mutex{},
		},
	}}

	started, failures := commandSessions(store, []string{"conta"}, "fishing:start:jardim-flutuante", "iniciando-pesca", "jardim-flutuante")
	if len(failures) != 0 || len(started) != 1 {
		t.Fatalf("início recusado: iniciadas=%v falhas=%v", started, failures)
	}
	if got := strings.TrimSpace(control.String()); got != "fishing:start:jardim-flutuante" {
		t.Fatalf("comando = %q; esperado destino completo", got)
	}
	if got := store.sessions["conta"].FishingDestination; got != "jardim-flutuante" {
		t.Fatalf("destino persistido = %q", got)
	}
}

func TestNormalizaDestinoPadraoERecusaDesconhecido(t *testing.T) {
	t.Parallel()
	if got, ok := normalizeFishingDestination(""); !ok || got != "infobus" {
		t.Fatalf("destino padrão = %q, %v", got, ok)
	}
	if _, ok := normalizeFishingDestination("quarto-inventado"); ok {
		t.Fatal("destino desconhecido foi aceito")
	}
}

func TestParadaSoConfirmaDepoisDoMarcoDoMotor(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-2 * time.Second)
	store := &sessionStore{sessions: map[string]session{
		"conta": {ID: "conta", Status: "vara-lançada", Automation: "parando", AutomationStartedAt: &startedAt},
	}}
	go func() {
		time.Sleep(20 * time.Millisecond)
		store.mu.Lock()
		item := store.sessions["conta"]
		item.Status = "conectada"
		finishAutomation(&item, time.Now())
		store.sessions["conta"] = item
		store.mu.Unlock()
	}()

	confirmed, failures := waitForAutomationStop(store, []string{"conta"}, time.Second)
	if len(failures) != 0 || len(confirmed) != 1 {
		t.Fatalf("confirmação inesperada: confirmadas=%v falhas=%v", confirmed, failures)
	}
	item := store.sessions["conta"]
	if item.Automation != "" || item.AutomationStartedAt != nil || item.AutomationSeconds < 1 {
		t.Fatalf("estado final inválido: %+v", item)
	}
}

func TestPreparaMensagemGlobalComAcentos(t *testing.T) {
	command, err := prepareGlobalChatCommand("Olá, pessoal! 🎣")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(command, globalChatCommandPrefix) {
		t.Fatalf("prefixo ausente: %q", command)
	}
}

func TestMensagemGlobalRecusaQuebraDeLinha(t *testing.T) {
	if _, err := prepareGlobalChatCommand("primeira\nsegunda"); err == nil {
		t.Fatal("mensagem com quebra de linha foi aceita")
	}
}

func TestReconheceSessaoDentroDoQuarto(t *testing.T) {
	if !isSessionInsideRoom("vara-lançada") || !isSessionInsideRoom("festejando") {
		t.Fatal("status dentro de quarto não reconhecido")
	}
	if isSessionInsideRoom("conectada") || isSessionInsideRoom("desconectada") {
		t.Fatal("status fora de quarto foi aceito")
	}
}
