package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIdentificaContaPeloEventoMaisRecente(t *testing.T) {
	t.Parallel()
	events := []string{
		"MARCO_OK: ACCOUNT_IDENTIFIED nome=Conta Antiga",
		"TRACE_ROOM header=34 bytes=24",
		"MARCO_OK: ACCOUNT_IDENTIFIED nome=NomeReal",
	}
	if got := identifiedAccount(events); got != "NomeReal" {
		t.Fatalf("identifiedAccount() = %q; esperado NomeReal", got)
	}
}

func TestNovosEventosNaoRecontaMarcadoresComTracesRepetidos(t *testing.T) {
	t.Parallel()
	previous := []string{
		"TRACE_ROOM header=34 bytes=24",
		"MARCO_OK: CAST_SENT posição=1,1 alvo=2,2",
		"TRACE_ROOM header=34 bytes=24",
	}
	current := []string{
		"TRACE_ROOM header=34 bytes=24",
		"MARCO_OK: CAST_SENT posição=1,1 alvo=2,2",
		"TRACE_ROOM header=34 bytes=24",
		"MARCO_OK: MORDIDA_PUXADA estado=0->1 alvo=7",
	}

	got := newEventsByOccurrence(previous, current)
	if len(got) != 1 || got[0] != "MARCO_OK: MORDIDA_PUXADA estado=0->1 alvo=7" {
		t.Fatalf("newEventsByOccurrence() = %#v", got)
	}
}

func TestNovosEventosPreservaOcorrenciasLegitimasDuplicadas(t *testing.T) {
	t.Parallel()
	previous := []string{"TRACE_ROOM header=34 bytes=24"}
	current := []string{
		"TRACE_ROOM header=34 bytes=24",
		"TRACE_ROOM header=34 bytes=24",
	}

	got := newEventsByOccurrence(previous, current)
	if len(got) != 1 || got[0] != "TRACE_ROOM header=34 bytes=24" {
		t.Fatalf("newEventsByOccurrence() = %#v", got)
	}
}

func TestNovosEventosContaCapturaRepetidaNoFimDaJanela(t *testing.T) {
	t.Parallel()
	previous := []string{
		"MARCO_OK: FISHING_CHAT Você pegou um Tadpole! (+11 EXP)",
		"MARCO_OK: FISHING_STATE estado=0 alvo=10",
	}
	current := []string{
		"MARCO_OK: FISHING_STATE estado=0 alvo=10",
		"MARCO_OK: FISHING_CHAT Você pegou um Tadpole! (+11 EXP)",
	}

	got := newEventsByOccurrence(previous, current)
	if len(got) != 1 || got[0] != current[1] {
		t.Fatalf("captura repetida nova foi perdida: %#v", got)
	}
}

func TestResetNaoRecontaEventosAnterioresDaSessaoAtual(t *testing.T) {
	c := &collector{data: newPersisted(time.Now()), dataPath: filepath.Join(t.TempDir(), "reports.json")}
	session := engineSession{
		ID:                "sessao-1",
		Nickname:          "ContaTeste",
		Automation:        "pesca",
		AutomationSeconds: 90,
		Events:            []string{"MARCO_OK: pegou um Peixe-Teste! + 10 EXP"},
	}
	c.consume([]engineSession{session})
	if c.snapshot().Totals.Captures != 1 {
		t.Fatal("a captura inicial deveria ser contabilizada")
	}

	c.reset()
	if got := c.snapshot(); got.Totals.Captures != 0 || got.Totals.XP != 0 || len(got.Accounts) != 0 {
		t.Fatalf("reset() preservou dados: %#v", got)
	}

	// A mesma janela de eventos ainda visível no motor não pode entrar de novo.
	c.consume([]engineSession{session})
	if got := c.snapshot().Totals.Captures; got != 0 {
		t.Fatalf("captura anterior foi recontada após reset: %d", got)
	}
}

func TestNivelDaAreaEConfirmacaoExata(t *testing.T) {
	account := &accountReport{}
	if !updateFishingLevelFromDestination(account, "jardim-flutuante") || account.FishingLevelMin != 30 {
		t.Fatalf("nível mínimo da área não identificado: %#v", account)
	}
	if !updateFishingLevelFromEvent(account, "Você alcançou o nível 42 de pesca") {
		t.Fatal("evento de nível exato não foi identificado")
	}
	if account.FishingLevel == nil || *account.FishingLevel != 42 || account.FishingLevelSource != "informado pelo hotel" {
		t.Fatalf("nível exato incorreto: %#v", account)
	}
}

func TestNivelVemDaConsultaDiretaDeEstatisticas(t *testing.T) {
	account := &accountReport{}
	event := "MARCO_OK: FISHING_STATS nivel=42 maximo=100 xpTotal=91500 xpNivelAtual=84512 xpProximoNivel=93900 peixes=3210 dourados=18"
	if !updateFishingLevelFromEvent(account, event) {
		t.Fatal("a consulta de estatísticas deveria atualizar a conta")
	}
	if account.FishingLevel == nil || *account.FishingLevel != 42 || account.FishingTotalXP != 91500 || account.FishingXPForNextLevel != 93900 || account.FishingFishesCaught != 3210 || account.FishingGoldenFishesCaught != 18 {
		t.Fatalf("estatísticas incorretas: %#v", account)
	}
}

func TestRelatorioAgrupaReconexoesPeloPerfil(t *testing.T) {
	now := time.Now().UTC()
	c := &collector{data: newPersisted(now), dataPath: filepath.Join(t.TempDir(), "reports.json")}
	first := engineSession{
		ID:                "sessao-antiga",
		ProfileID:         "perfil-nubank",
		Nickname:          "Nubank",
		Hotel:             "origins",
		CreatedAt:         now.Add(-time.Minute),
		Automation:        "pesca",
		AutomationSeconds: 60,
		Events:            []string{"MARCO_OK: pegou um Tadpole! + 10 EXP"},
	}
	second := engineSession{
		ID:                "sessao-nova",
		ProfileID:         "perfil-nubank",
		Nickname:          "Nubank",
		Hotel:             "origins",
		CreatedAt:         now,
		Automation:        "pesca",
		AutomationSeconds: 90,
		Events:            []string{"MARCO_OK: pegou um Carp! + 15 EXP"},
	}
	c.consume([]engineSession{first})
	c.consume([]engineSession{first, second})

	report := c.snapshot()
	if len(report.Accounts) != 1 {
		t.Fatalf("reconexão criou %d cartões; esperado 1", len(report.Accounts))
	}
	account := report.Accounts[0]
	if account.AccountID != "profile:perfil-nubank" || account.Captures != 2 || account.Sessions != 2 {
		t.Fatalf("agrupamento incorreto: %#v", account)
	}
	if account.SessionID != second.ID {
		t.Fatalf("sessão mais nova não ficou visível: %q", account.SessionID)
	}
	if history := c.accountHistory(account.AccountID); len(history) != 2 {
		t.Fatalf("histórico da conta = %d; esperado 2", len(history))
	}
}

func TestDiagnosticoDeTimeoutDistingueAsFases(t *testing.T) {
	c := &collector{data: newPersisted(time.Now())}
	account := &accountReport{AccountID: "profile:teste", Fish: map[string]int{}}
	session := engineSession{ID: "sessao"}
	for _, event := range []string{
		"MARCO_OK: FISHING_TIMEOUT fase=lancamento-enviado nova tentativa",
		"MARCO_OK: FISHING_TIMEOUT fase=aguardando-mordida nova tentativa",
		"MARCO_OK: FISHING_TIMEOUT fase=aguardando-resultado nova tentativa",
		"MARCO_OK: FISHING_TARGET_BUSY id=8 por=2.5s",
	} {
		if !c.consumeEvent(account, session, event) {
			t.Fatalf("evento não foi contabilizado: %s", event)
		}
	}
	if account.Timeouts != 3 || account.NoAckTimeouts != 1 || account.BiteTimeouts != 1 || account.ResultTimeouts != 1 || account.TargetContentions != 1 {
		t.Fatalf("diagnóstico de timeout incorreto: %#v", account)
	}
}

func TestMigracaoMantemCapturasNaIdentidadeEstavel(t *testing.T) {
	c := &collector{data: newPersisted(time.Now())}
	c.data.Accounts["sessao-velha"] = &accountReport{
		SessionID: "sessao-velha",
		Account:   "Conta Histórica",
		Captures:  3,
		Fish:      map[string]int{"Tadpole": 3},
	}
	c.data.Captures = []capture{{SessionID: "sessao-velha", Account: "Conta Histórica", Fish: "Tadpole", XP: 10}}
	c.migrateAccountsByIdentity()
	account := c.data.Accounts["nick:origins:conta histórica"]
	if account == nil || account.Captures != 3 {
		t.Fatalf("métricas históricas não foram preservadas: %#v", c.data.Accounts)
	}
	if got := c.data.Captures[0].AccountID; got != "nick:origins:conta histórica" {
		t.Fatalf("captura migrou para %q", got)
	}
}

func TestProfileIDAbsorveHistoricoAntigoDoMesmoApelido(t *testing.T) {
	c := &collector{data: newPersisted(time.Now())}
	legacyID := "nick:origins:conta teste"
	c.data.Accounts[legacyID] = &accountReport{
		AccountID: legacyID,
		SessionID: "sessao-antiga",
		Account:   "Conta Teste",
		Captures:  7,
		XP:        70,
		Fish:      map[string]int{"Tadpole": 7},
	}
	c.data.SessionAccounts["sessao-antiga"] = legacyID
	account := c.account(engineSession{
		ID:        "sessao-nova",
		ProfileID: "perfil-confirmado",
		Nickname:  "Conta Teste",
		Hotel:     "origins",
	})
	if len(c.data.Accounts) != 1 || account.AccountID != "profile:perfil-confirmado" || account.Captures != 7 {
		t.Fatalf("histórico não foi absorvido pelo perfil: %#v", c.data.Accounts)
	}
	if got := c.data.SessionAccounts["sessao-antiga"]; got != account.AccountID {
		t.Fatalf("sessão histórica permaneceu em %q", got)
	}
}

func TestInicializacaoConsolidaPerfilEApelidoPersistidos(t *testing.T) {
	c := &collector{data: newPersisted(time.Now())}
	legacyID := "nick:origins:conta teste"
	profileID := "profile:perfil-confirmado"
	c.data.Accounts[legacyID] = &accountReport{
		AccountID: legacyID,
		SessionID: "sessao-antiga",
		Account:   "Conta Teste",
		Captures:  9,
		Fish:      map[string]int{"Tadpole": 9},
	}
	c.data.Accounts[profileID] = &accountReport{
		AccountID: profileID,
		SessionID: "sessao-nova",
		Account:   "Conta Teste",
		Fish:      map[string]int{},
	}
	c.mergePersistedProfileAliases()
	if len(c.data.Accounts) != 1 || c.data.Accounts[profileID].Captures != 9 {
		t.Fatalf("aliases persistidos não foram consolidados: %#v", c.data.Accounts)
	}
}
