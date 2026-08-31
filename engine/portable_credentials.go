package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	portableCredentialPrefix  = "portable:v1:"
	portableCredentialKeyName = "credentials.key"
)

// exportPortableProfiles é uma migração local: a origem continua cifrada por
// DPAPI e apenas a cópia de destino recebe uma chave própria do pacote.
func exportPortableProfiles(sourcePath, destinationPath, keyPath string) error {
	profiles, err := loadProfiles(sourcePath)
	if err != nil {
		return err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("não foi possível gerar a chave do pacote: %w", err)
	}
	for index := range profiles {
		if profiles[index].Password != "" {
			plain, err := unprotect(profiles[index].Password)
			if err != nil {
				return fmt.Errorf("não foi possível migrar um login salvo: %w", err)
			}
			profiles[index].Password, err = protectPortable(plain, key)
			if err != nil {
				return err
			}
		}
		if profiles[index].TOTP != "" {
			plain, err := unprotect(profiles[index].TOTP)
			if err != nil {
				return fmt.Errorf("não foi possível migrar o 2FA salvo: %w", err)
			}
			profiles[index].TOTP, err = protectPortable(plain, key)
			if err != nil {
				return err
			}
		}
	}
	if err := saveProfiles(destinationPath, profiles); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return err
	}
	return os.WriteFile(keyPath, key, 0600)
}

func protectPortable(value string, key []byte) (string, error) {
	if value == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	payload := gcm.Seal(nonce, nonce, []byte(value), nil)
	return portableCredentialPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func unprotectPortable(value, keyPath string) (string, error) {
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, portableCredentialPrefix))
	if err != nil {
		return "", fmt.Errorf("credencial portátil inválida: %w", err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("chave das credenciais portáteis não encontrada: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("chave das credenciais portáteis inválida: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("credencial portátil incompleta")
	}
	plain, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("não foi possível abrir a credencial portátil: %w", err)
	}
	return string(plain), nil
}
