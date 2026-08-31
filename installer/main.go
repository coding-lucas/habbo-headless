package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// payload.zip é o pacote portátil completo, incorporado no instalador.
//
//go:embed payload.zip
var payload []byte

const packageFolder = "Habbo Headless Servidor 20260826-230807"

func main() {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	target := filepath.Join(localAppData, "Habbo Headless")
	if err := extractPayload(target); err != nil {
		showMessage("Não foi possível instalar o Habbo Headless:\n"+err.Error(), 0x10)
		return
	}
	application := filepath.Join(target, packageFolder, "Habbo Headless.exe")
	if _, err := os.Stat(application); err != nil {
		showMessage("A instalação terminou, mas o aplicativo principal não foi encontrado.", 0x10)
		return
	}
	if err := exec.Command(application).Start(); err != nil {
		showMessage("Instalado, mas não foi possível iniciar o aplicativo:\n"+err.Error(), 0x10)
		return
	}
	showMessage("Habbo Headless instalado e iniciado.\n\nLocal: "+target, 0x40)
}

func extractPayload(target string) error {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("pacote interno inválido: %w", err)
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	prefix := filepath.Clean(target) + string(os.PathSeparator)
	for _, entry := range reader.File {
		destination := filepath.Join(target, filepath.FromSlash(entry.Name))
		cleanDestination := filepath.Clean(destination)
		if cleanDestination != filepath.Clean(target) && !strings.HasPrefix(cleanDestination, prefix) {
			return fmt.Errorf("entrada inválida no pacote")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanDestination, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanDestination), 0700); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(cleanDestination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err == nil {
			_, err = io.Copy(output, source)
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
		_ = source.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func showMessage(message string, flags uintptr) {
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("Instalador Habbo Headless")
	user32 := syscall.NewLazyDLL("user32.dll")
	_, _, _ = user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), flags)
}
