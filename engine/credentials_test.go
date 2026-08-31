package main

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestCredencialPortatilRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), portableCredentialKeyName)
	if err := os.WriteFile(path, key, 0600); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := protectPortable("senha de teste", key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := unprotectPortable(ciphertext, path)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "senha de teste" {
		t.Fatalf("credencial portátil diferente: %q", plain)
	}
}

func TestProtectRoundTrip(t *testing.T) {
	for _, value := range []string{"codex-teste-123", "senha çãõ !@#$%"} {
		encrypted, err := protect(value)
		if err != nil {
			t.Fatalf("protect(%q): %v", value, err)
		}
		plain, err := unprotect(encrypted)
		if err != nil {
			t.Fatalf("unprotect(%q): %v", value, err)
		}
		if plain != value {
			t.Fatalf("round-trip: recebido %q, esperado %q", plain, value)
		}
	}
}

func TestPerfilSoEhSubstituidoPorEmailAposAutenticacao(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saved-logins.json")
	old := savedProfile{ID: "antigo", Nickname: "Nome antigo", Mode: "habbo", Email: "conta", Password: "cifra-antiga"}
	if err := saveProfiles(path, []savedProfile{old}); err != nil {
		t.Fatal(err)
	}
	current := savedProfile{ID: savedProfileID("CONTA"), Nickname: "Nome real", Mode: "habbo", Email: "CONTA", Password: "cifra-nova"}
	if err := upsertAuthenticatedProfile(path, current); err != nil {
		t.Fatal(err)
	}
	profiles, err := loadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Nickname != "Nome real" || profiles[0].Password != "cifra-nova" {
		t.Fatalf("perfil autenticado não substituiu o anterior: %#v", profiles)
	}
}

func TestIdentificadorDePerfilEhEstavel(t *testing.T) {
	if savedProfileID(" Conta ") != savedProfileID("conta") {
		t.Fatal("identificador mudou apenas por caixa ou espaços")
	}
	if savedProfileID("conta") == savedProfileID("outra") {
		t.Fatal("contas diferentes receberam o mesmo identificador")
	}
}

func TestValidacaoDaURLDeAutorizacaoSteam(t *testing.T) {
	validas := []string{
		"https://steamcommunity.com/openid/login?token=temporario",
		"https://origins.habbo.com.br/steam/auth",
	}
	for _, value := range validas {
		if err := validateSteamAuthorizationURL(value); err != nil {
			t.Fatalf("URL oficial deveria ser aceita (%s): %v", value, err)
		}
	}
	invalidas := []string{
		"http://steamcommunity.com/openid/login",
		"https://steamcommunity.com.exemplo.com/login",
		"https://exemplo.com/login",
		"javascript:alert(1)",
	}
	for _, value := range invalidas {
		if err := validateSteamAuthorizationURL(value); err == nil {
			t.Fatalf("URL não oficial deveria ser recusada: %s", value)
		}
	}
}
