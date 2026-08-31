package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	engineHealth   = "http://127.0.0.1:8787/api/health"
	reporterHealth = "http://127.0.0.1:8788/api/health"
	dashboardURL   = "http://127.0.0.1:3000"
	createNoWindow = 0x08000000
)

func main() {
	root, err := locateProjectRoot()
	if err != nil {
		showError(err)
		return
	}
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		showError(fmt.Errorf("não foi possível preparar os registros: %w", err))
		return
	}

	if !endpointOnline(engineHealth) {
		if err := startHidden(root, "motor", releaseComponent(root, "engine", "engine.exe", "engine.performance-v2.exe")); err != nil {
			showError(err)
			return
		}
		if !waitForEndpoint(engineHealth, 15*time.Second) {
			showError(fmt.Errorf("o motor não ficou disponível; consulte logs\\launcher-motor.log"))
			return
		}
	}

	if !endpointOnline(reporterHealth) {
		if err := startHidden(root, "relatorios", releaseComponent(root, "bin", "habbo-reporter.exe", "habbo-reporter.performance-v2.exe")); err != nil {
			showError(err)
			return
		}
		if !waitForEndpoint(reporterHealth, 15*time.Second) {
			showError(fmt.Errorf("os relatórios não ficaram disponíveis; consulte logs\\launcher-relatorios.log"))
			return
		}
	}

	if !endpointOnline(dashboardURL) {
		node, err := locateNode(root)
		if err != nil {
			showError(err)
			return
		}
		vinext := filepath.Join(root, "node_modules", "vinext", "dist", "cli.js")
		if err := startHidden(root, "dashboard", node, vinext, "start"); err != nil {
			showError(err)
			return
		}
		if !waitForEndpoint(dashboardURL, 35*time.Second) {
			showError(fmt.Errorf("o dashboard não ficou disponível; consulte logs\\launcher-dashboard.log"))
			return
		}
	}

	if !hasArgument("--no-browser") {
		_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", dashboardURL).Start()
	}
}

// releaseComponent mantém a atualização em paralelo até que a sessão atual
// termine. Ao abrir o executável com o sufixo da revisão, ele seleciona os
// componentes compilados na mesma versão; o executável comum continua intacto
// como retorno seguro.
func releaseComponent(root, folder, standard, versioned string) string {
	standardPath := filepath.Join(root, folder, standard)
	if !runningPerformanceV2() {
		return standardPath
	}
	versionedPath := filepath.Join(root, folder, versioned)
	if fileExists(versionedPath) {
		return versionedPath
	}
	return standardPath
}

func runningPerformanceV2() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(filepath.Base(executable)), "performance-v2")
}

// locateNode prioriza o runtime incluído no pacote portátil. A instalação
// global do Windows continua sendo apenas uma alternativa para desenvolvimento.
func locateNode(root string) (string, error) {
	portable := filepath.Join(root, "runtime", "node", "node.exe")
	if fileExists(portable) {
		return portable, nil
	}
	node, err := exec.LookPath("node.exe")
	if err != nil {
		return "", fmt.Errorf("Node.js não foi encontrado neste Windows")
	}
	return node, nil
}

func locateProjectRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executableDir := filepath.Dir(executable)
	candidates := []string{
		executableDir,
		filepath.Join(executableDir, "HABBO HEADLESS"),
		filepath.Dir(executableDir),
		filepath.Join(filepath.Dir(executableDir), "HABBO HEADLESS"),
	}
	for _, candidate := range candidates {
		if fileExists(filepath.Join(candidate, "package.json")) &&
			fileExists(filepath.Join(candidate, "engine", "engine.exe")) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("a pasta interna HABBO HEADLESS não foi encontrada ao lado do aplicativo")
}

func startHidden(root, logName, executable string, args ...string) error {
	if !fileExists(executable) {
		return fmt.Errorf("componente não encontrado: %s", executable)
	}
	logPath := filepath.Join(root, "logs", "launcher-"+logName+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("não foi possível abrir %s: %w", logPath, err)
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NODE_ENV=production")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("não foi possível iniciar %s: %w", logName, err)
	}
	_ = logFile.Close()
	return nil
}

func endpointOnline(address string) bool {
	client := &http.Client{Timeout: 900 * time.Millisecond}
	response, err := client.Get(address)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 500
}

func waitForEndpoint(address string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if endpointOnline(address) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasArgument(expected string) bool {
	for _, argument := range os.Args[1:] {
		if strings.EqualFold(argument, expected) {
			return true
		}
	}
	return false
}

func showError(err error) {
	message, _ := syscall.UTF16PtrFromString(err.Error())
	title, _ := syscall.UTF16PtrFromString("Habbo Headless")
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	_, _, _ = messageBox.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
