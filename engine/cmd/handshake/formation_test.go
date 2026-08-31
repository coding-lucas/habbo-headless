package main

import "testing"

func TestFigurasDeFormacaoPossuemTrintaECincoPosicoes(t *testing.T) {
	figuras := []string{"fila", "coracao", "estrela", "coroa", "peixe", "trofeu", "smile", "help"}
	for _, figura := range figuras {
		cells, _, _, err := formationCellsFor(figura)
		if err != nil {
			t.Fatalf("%s: %v", figura, err)
		}
		if len(cells) != 35 {
			t.Fatalf("%s: recebeu %d posições, queria 35", figura, len(cells))
		}
	}
}

func TestComandoDeFormacaoExigeTrintaECincoContas(t *testing.T) {
	_, _, _, _, err := parseFormationStartCommand("formation:start:123:0:0:0:34")
	if err == nil {
		t.Fatal("deveria recusar uma formação diferente de 35 contas")
	}
	roomID, port, door, config, err := parseFormationStartCommand("formation:start:123:0:0:34:35")
	if err != nil || roomID != 123 || port != 0 || door != 0 || config.Slot != 34 || config.Total != 35 {
		t.Fatalf("comando válido interpretado incorretamente: room=%d port=%d door=%d config=%+v err=%v", roomID, port, door, config, err)
	}
}
