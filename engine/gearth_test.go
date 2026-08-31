package main

import (
	"net"
	"testing"
)

func TestResolvePublicOriginsHost(t *testing.T) {
	address, err := resolvePublicOriginsHost()
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(address)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() {
		t.Fatalf("endereço público inválido: %q", address)
	}
}

func TestDetectaRedirecionamentoDoGearth(t *testing.T) {
	t.Parallel()
	hosts := []byte("# comentário\r\n127.0.0.3 game-obr.habbo.com\r\n")
	if !containsGEarthRedirect(hosts) {
		t.Fatal("não detectou o redirecionamento Origins do G-Earth")
	}
}

func TestIgnoraEntradaComentadaDoGearth(t *testing.T) {
	t.Parallel()
	hosts := []byte("# 127.0.0.3 game-obr.habbo.com\r\n127.0.0.1 localhost\r\n")
	if containsGEarthRedirect(hosts) {
		t.Fatal("considerou como ativo um redirecionamento comentado")
	}
}
