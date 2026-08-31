package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const originsHost = "game-obr.habbo.com"
const originsPort = 40001

type gearthStatus struct {
	Running   bool   `json:"running"`
	Ready     bool   `json:"ready"`
	Connected bool   `json:"connected"`
	Instances int    `json:"instances"`
	State     string `json:"state"`
}

// Cada sessão possui seu próprio proxy invisível, com portas isoladas e ciclo
// de vida controlado pelo Habbo Headless.
func currentGEarthStatus(store *sessionStore) gearthStatus {
	status := gearthStatus{Running: true, Ready: true, State: "integrado e pronto"}
	store.mu.RLock()
	for _, item := range store.sessions {
		if item.gearthProcess != nil && item.Status != "desconectada" && item.Status != "encerrada" {
			status.Instances++
		}
	}
	store.mu.RUnlock()
	status.Connected = status.Instances > 0
	if status.Connected {
		status.State = fmt.Sprintf("%d núcleo(s) Codex G-Earth ativo(s)", status.Instances)
	}
	return status
}

func codexGEarthRoot() string {
	if value := strings.TrimSpace(os.Getenv("HABBO_CODEX_GEARTH_DIR")); value != "" {
		return value
	}
	root, err := filepath.Abs(filepath.Join("runtime", "codex-gearth"))
	if err != nil {
		return filepath.Join("runtime", "codex-gearth")
	}
	return root
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func resolvePublicOriginsHost() (string, error) {
	for _, dnsServer := range []string{"1.1.1.1", "8.8.8.8"} {
		script := fmt.Sprintf("$ErrorActionPreference='Stop'; Resolve-DnsName '%s' -Type A -Server '%s' -DnsOnly | Where-Object Type -eq A | Select-Object -ExpandProperty IPAddress", originsHost, dnsServer)
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		cmd.Env = windowsPowerShellEnv()
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		for _, address := range strings.Fields(string(output)) {
			ip := net.ParseIP(address)
			if ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() {
				return address, nil
			}
		}
	}
	return "", fmt.Errorf("não foi possível resolver o endereço público de %s", originsHost)
}

func launchCodexGEarth(proxyPort, requestedExtensionPort int, targetHost string) (*exec.Cmd, int, error) {
	root := codexGEarthRoot()
	java := filepath.Join(root, "jre", "bin", "java.exe")
	core := filepath.Join(root, "Codex-G-Earth.jar")
	api := filepath.Join(root, "G-Earth-Api.jar")
	for _, required := range []string{java, core, api} {
		if _, err := os.Stat(required); err != nil {
			return nil, 0, fmt.Errorf("componente Codex G-Earth ausente em %s: %w", required, err)
		}
	}

	classpath := strings.Join([]string{core, api, filepath.Join(root, "libs", "*")}, ";")
	cmd := exec.Command(java,
		"-cp", classpath,
		"gearth.app.CodexGEarthMain",
		"--listen-host", "127.0.0.1",
		"--listen-port", strconv.Itoa(proxyPort),
		"--target-host", targetHost,
		"--target-port", strconv.Itoa(originsPort),
		"--extension-port", strconv.Itoa(requestedExtensionPort),
	)
	cmd.Dir = root
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, 0, fmt.Errorf("não foi possível iniciar o Codex G-Earth: %w", err)
	}

	ready := make(chan int, 1)
	finished := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[codex-gearth:%d] %s", proxyPort, line)
			if !strings.Contains(line, "CODEX_GEARTH_READY") {
				continue
			}
			for _, field := range strings.Fields(line) {
				if strings.HasPrefix(field, "extension=") {
					if port, parseErr := strconv.Atoi(strings.TrimPrefix(field, "extension=")); parseErr == nil {
						ready <- port
					}
				}
			}
		}
		finished <- struct{}{}
	}()

	select {
	case extensionPort := <-ready:
		return cmd, extensionPort, nil
	case <-finished:
		_ = cmd.Wait()
		return nil, 0, fmt.Errorf("o núcleo Codex G-Earth encerrou antes de ficar pronto")
	case <-time.After(12 * time.Second):
		_ = cmd.Process.Kill()
		return nil, 0, fmt.Errorf("o núcleo Codex G-Earth não ficou pronto a tempo")
	}
}

func launchGEarthFishingExtension(extensionPort int) (*exec.Cmd, error) {
	executable := filepath.Join(codexGEarthRoot(), "extensions", "pesca.exe")
	if _, err := os.Stat(executable); err != nil {
		return nil, fmt.Errorf("extensão de pesca integrada não encontrada em %s: %w", executable, err)
	}
	cmd := exec.Command(executable, "-p", strconv.Itoa(extensionPort))
	cmd.Dir = filepath.Dir(executable)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("não foi possível iniciar a extensão de pesca integrada: %w", err)
	}
	return cmd, nil
}

// A extensão de visual é iniciada antes do handshake. Ela aguarda o
// USER_OBJ natural do login, aplica o uniforme Codex uma única vez e encerra.
// Portanto, funciona tanto para contas Habbo quanto para contas Steam e não
// depende de a automação de pesca estar ligada.
func launchGEarthSkinExtension(extensionPort int, figure string) (*exec.Cmd, error) {
	executable := filepath.Join(codexGEarthRoot(), "extensions", "skin.exe")
	if _, err := os.Stat(executable); err != nil {
		return nil, fmt.Errorf("extensão de visual integrada não encontrada em %s: %w", executable, err)
	}
	cmd := exec.Command(executable, strconv.Itoa(extensionPort), figure)
	cmd.Dir = filepath.Dir(executable)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("não foi possível iniciar a extensão de visual integrada: %w", err)
	}
	return cmd, nil
}

// Mantida para cobrir configurações antigas nos testes de migração. O motor
// novo não altera nem consulta mais o arquivo hosts.
func containsGEarthRedirect(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "127.") {
			continue
		}
		for _, host := range fields[1:] {
			if strings.EqualFold(host, originsHost) {
				return true
			}
		}
	}
	return false
}
