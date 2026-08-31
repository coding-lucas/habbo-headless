package main

import "testing"

func TestFishingDestinationForLevel(t *testing.T) {
	cases := []struct {
		level int
		want  string
	}{
		{0, "infobus"},
		{1, "infobus"},
		{29, "infobus"},
		{30, "jardim-flutuante"},
		{69, "jardim-flutuante"},
		{70, "snouthill-pier"},
	}
	for _, tc := range cases {
		if got := fishingDestinationForLevel(tc.level); got != tc.want {
			t.Errorf("nível %d: destino %q; queria %q", tc.level, got, tc.want)
		}
	}
}

func TestNormalizeFishingMode(t *testing.T) {
	cases := map[string]string{"": fishingModeManual, "manual": fishingModeManual, " POR-NIVEL ": fishingModeByLevel}
	for raw, want := range cases {
		got, ok := normalizeFishingMode(raw)
		if !ok || got != want {
			t.Errorf("modo %q: resultado %q, válido=%v", raw, got, ok)
		}
	}
	if _, ok := normalizeFishingMode("qualquer-coisa"); ok {
		t.Fatal("modo inválido foi aceito")
	}
}

func TestNivelAtualDaSessaoTemPrioridadeSobreHistorico(t *testing.T) {
	item := session{Nickname: ":Nubank", Events: []string{
		"MARCO_OK: FISHING_STATS nivel=34 maximo=99 xpTotal=41592 xpNivelAtual=40474 xpProximoNivel=46855 peixes=1555 dourados=19",
	}}
	level, source := fishingLevelForSession(item, map[string]int{":nubank": 12})
	if level != 34 || source != "sessão atual" {
		t.Fatalf("nível resolvido incorretamente: %d (%s)", level, source)
	}
}

func TestNivelDaSessaoLeUltimaLeituraValida(t *testing.T) {
	level := fishingLevelFromSession([]string{
		"MARCO_OK: FISHING_STATS nivel=12 maximo=99 xpTotal=3267",
		"MARCO_OK: FISHING_STATS nivel=34 maximo=99 xpTotal=41592",
	})
	if level != 34 {
		t.Fatalf("nível atual = %d; esperado 34", level)
	}
}
