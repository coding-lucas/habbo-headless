package main

import (
	"testing"
	"time"
)

func TestReservaDePeixeImpedeConcorrenciaELiberaAoFim(t *testing.T) {
	coordinator := newFishingCoordinator()
	first := fishingLeaseRequest{SessionID: "conta-a", RoomKey: "infobus:1:1", TargetID: 99, LeaseMS: 45000}
	if !coordinator.reserve(first).Reserved {
		t.Fatal("a primeira conta deveria reservar o alvo")
	}
	if coordinator.reserve(fishingLeaseRequest{SessionID: "conta-b", RoomKey: first.RoomKey, TargetID: first.TargetID, LeaseMS: 45000}).Reserved {
		t.Fatal("duas contas reservaram o mesmo alvo")
	}
	coordinator.release(fishingReleaseRequest{SessionID: "conta-b", RoomKey: first.RoomKey, TargetID: first.TargetID})
	if coordinator.reserve(fishingLeaseRequest{SessionID: "conta-b", RoomKey: first.RoomKey, TargetID: first.TargetID, LeaseMS: 45000}).Reserved {
		t.Fatal("uma conta diferente liberou a reserva que não possuía")
	}
	coordinator.release(fishingReleaseRequest{SessionID: "conta-a", RoomKey: first.RoomKey, TargetID: first.TargetID})
	if !coordinator.reserve(fishingLeaseRequest{SessionID: "conta-b", RoomKey: first.RoomKey, TargetID: first.TargetID, LeaseMS: 45000}).Reserved {
		t.Fatal("o alvo não voltou a ficar disponível após a liberação")
	}
}

func TestReservaExpiradaNaoPrendePeixe(t *testing.T) {
	coordinator := newFishingCoordinator()
	key := "infobus:1:1#77"
	coordinator.targets[key] = fishingTargetLease{SessionID: "conta-antiga", ExpiresAt: time.Now().Add(-time.Second)}
	if !coordinator.reserve(fishingLeaseRequest{SessionID: "conta-nova", RoomKey: "infobus:1:1", TargetID: 77, LeaseMS: 45000}).Reserved {
		t.Fatal("reserva vencida continuou bloqueando o peixe")
	}
}

func TestDistribuicaoDeSalaPriorizaSepararBots(t *testing.T) {
	coordinator := newFishingCoordinator()
	candidates := []fishingRoomCandidate{
		{Key: "infobus:1:1", Name: "Infobus A", Users: 2, Capacity: 10},
		{Key: "infobus:2:2", Name: "Infobus B", Users: 7, Capacity: 10},
	}
	first := coordinator.chooseRoom(fishingRoomClaimRequest{SessionID: "conta-a", Destination: "infobus", Candidates: candidates})
	if first.RoomKey != "infobus:1:1" {
		t.Fatalf("primeira conta escolheu %q; esperada a sala menos ocupada", first.RoomKey)
	}
	second := coordinator.chooseRoom(fishingRoomClaimRequest{SessionID: "conta-b", Destination: "infobus", Candidates: candidates})
	if second.RoomKey != "infobus:2:2" {
		t.Fatalf("segunda conta não foi distribuída para outra sala: %q", second.RoomKey)
	}
}
