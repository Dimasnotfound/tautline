package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var quickTunnelURLPattern = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
var tunnelUUIDPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
var tunnelOriginPattern = regexp.MustCompile(`(?i)--url(?:=|\s+)(?:"([^"]+)"|'([^']+)'|([^\s]+))`)

type TunnelStatus struct {
	Available          bool   `json:"available"`
	Running            bool   `json:"running"`
	Mode               string `json:"mode"`
	Name               string `json:"name,omitempty"`
	PublicURL          string `json:"public_url,omitempty"`
	CustomDomain       string `json:"custom_domain,omitempty"`
	OriginURL          string `json:"origin_url,omitempty"`
	DetectedExternally bool   `json:"detected_externally,omitempty"`
	Executable         string `json:"executable,omitempty"`
	PID                int    `json:"pid,omitempty"`
	TunnelID           string `json:"tunnel_id,omitempty"`
	DNSTarget          string `json:"dns_target,omitempty"`
	LastError          string `json:"last_error,omitempty"`
}

type tunnelListItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type tunnelManager struct {
	mu              sync.Mutex
	store           *configStore
	command         *exec.Cmd
	logFile         *os.File
	mode            string
	publicURL       string
	tunnelID        string
	originURL       string
	externalPID     int
	externalAdopted bool
	lastError       string
	ready           chan struct{}
	readyOnce       sync.Once
}

func newTunnelManager(store *configStore) *tunnelManager {
	return &tunnelManager{store: store}
}

func (m *tunnelManager) resolveExecutable() (string, error) {
	cfg := m.store.snapshot().Tunnel
	candidates := []string{strings.TrimSpace(cfg.Executable)}
	if located, err := exec.LookPath(executableName("cloudflared")); err == nil {
		candidates = append(candidates, located)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(absolute)
		if err == nil && !info.IsDir() {
			return absolute, nil
		}
	}
	return "", fmt.Errorf("cloudflared was not found; place %s in bin or set TAUTLINE_CLOUDFLARED_PATH", executableName("cloudflared"))
}

type localTunnelProcess struct {
	ProcessID   int    `json:"ProcessId"`
	CommandLine string `json:"CommandLine"`
}

func detectLocalTunnelConnector(name string) (int, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, "", false
	}
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		script := `$items = @(Get-CimInstance Win32_Process | Where-Object { $_.Name -like 'cloudflared*' } | Select-Object ProcessId, CommandLine); $items | ConvertTo-Json -Compress`
		output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", script).Output()
		if err != nil || len(bytes.TrimSpace(output)) == 0 {
			return 0, "", false
		}
		var processes []localTunnelProcess
		trimmed := bytes.TrimSpace(output)
		if len(trimmed) > 0 && trimmed[0] == '{' {
			var single localTunnelProcess
			if json.Unmarshal(trimmed, &single) == nil {
				processes = append(processes, single)
			}
		} else {
			_ = json.Unmarshal(trimmed, &processes)
		}
		for _, process := range processes {
			if origin, matches := parseTunnelConnectorCommand(name, process.CommandLine); matches {
				return process.ProcessID, origin, true
			}
		}
		return 0, "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-eo", "pid=,args=").Output()
	if err != nil {
		return 0, "", false
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		commandLine := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		if origin, matches := parseTunnelConnectorCommand(name, commandLine); matches {
			return pid, origin, true
		}
	}
	return 0, "", false
}

func parseTunnelConnectorCommand(name, commandLine string) (string, bool) {
	lower := strings.ToLower(commandLine)
	if !strings.Contains(lower, "cloudflared") || !strings.Contains(lower, "tunnel") {
		return "", false
	}
	runPattern := regexp.MustCompile(`(?i)(?:^|\s)run\s+["']?` + regexp.QuoteMeta(strings.TrimSpace(name)) + `(?:["']?)(?:\s|$)`)
	if !runPattern.MatchString(commandLine) {
		return "", false
	}
	match := tunnelOriginPattern.FindStringSubmatch(commandLine)
	for index := 1; index < len(match); index++ {
		if strings.TrimSpace(match[index]) != "" {
			return strings.TrimSpace(match[index]), true
		}
	}
	return "", true
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
		return err == nil && strings.Contains(string(output), strconv.Itoa(pid))
	}
	return exec.Command("kill", "-0", strconv.Itoa(pid)).Run() == nil
}

func (m *tunnelManager) status() TunnelStatus {
	cfg := m.store.snapshot().Tunnel
	m.mu.Lock()
	mode := m.mode
	publicURL := m.publicURL
	tunnelID := m.tunnelID
	originURL := m.originURL
	lastError := m.lastError
	command := m.command
	externalPID := m.externalPID
	externalAdopted := m.externalAdopted
	m.mu.Unlock()

	status := TunnelStatus{
		Mode:         cfg.Mode,
		Name:         cfg.Name,
		PublicURL:    publicURL,
		CustomDomain: cfg.CustomDomain,
		OriginURL:    originURL,
		TunnelID:     tunnelID,
		LastError:    lastError,
	}
	if strings.TrimSpace(mode) != "" {
		status.Mode = mode
	}
	if status.PublicURL == "" && cfg.CustomDomain != "" {
		status.PublicURL = "https://" + strings.TrimPrefix(cfg.CustomDomain, "https://")
	}
	if tunnelID != "" {
		status.DNSTarget = tunnelID + ".cfargotunnel.com"
	}
	if executable, err := m.resolveExecutable(); err == nil {
		status.Available = true
		status.Executable = executable
	}
	if command != nil && command.Process != nil && command.ProcessState == nil {
		status.Running = true
		status.PID = command.Process.Pid
		return status
	}
	if externalPID > 0 && processExists(externalPID) {
		status.Running = true
		status.PID = externalPID
		status.DetectedExternally = externalAdopted
		return status
	}
	if strings.TrimSpace(cfg.Name) != "" {
		if pid, detectedOrigin, found := detectLocalTunnelConnector(cfg.Name); found {
			status.Running = true
			status.PID = pid
			status.OriginURL = detectedOrigin
			status.DetectedExternally = true
		}
	}
	return status
}

func (m *tunnelManager) start(mode string) error {
	m.mu.Lock()
	if m.command != nil && m.command.Process != nil && m.command.ProcessState == nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	cfg := m.store.snapshot()
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = cfg.Tunnel.Mode
	}
	if mode != "quick" && mode != "named" {
		return fmt.Errorf("tunnel mode must be quick or named")
	}
	origin := "http://127.0.0.1:" + cfg.Port
	if mode == "named" {
		if strings.TrimSpace(cfg.Tunnel.Name) == "" {
			return errors.New("named tunnel requires a tunnel name")
		}
		id, err := m.ensureNamedTunnel(cfg.Tunnel.Name)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.tunnelID = id
		m.mu.Unlock()
		if pid, existingOrigin, found := detectLocalTunnelConnector(cfg.Tunnel.Name); found {
			if strings.TrimRight(strings.ToLower(existingOrigin), "/") != strings.TrimRight(strings.ToLower(origin), "/") {
				return fmt.Errorf("tunnel %s is already connected to %s; expected %s", cfg.Tunnel.Name, existingOrigin, origin)
			}
			m.mu.Lock()
			m.mode = mode
			m.originURL = existingOrigin
			m.externalPID = pid
			m.externalAdopted = true
			m.lastError = ""
			if cfg.Tunnel.CustomDomain != "" {
				m.publicURL = "https://" + strings.TrimPrefix(cfg.Tunnel.CustomDomain, "https://")
			}
			m.mu.Unlock()
			return nil
		}
	}
	executable, err := m.resolveExecutable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(cfg.RuntimeDir, "logs"), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(cfg.RuntimeDir, "logs", "cloudflared.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	args := tunnelCommandArgs(cfg, mode, origin)
	command := exec.Command(executable, args...)
	command.Dir = mustWorkingDirectory()
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = logFile.Close()
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = logFile.Close()
		return err
	}
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return err
	}

	m.mu.Lock()
	m.command = command
	m.logFile = logFile
	m.mode = mode
	m.originURL = origin
	m.externalPID = 0
	m.externalAdopted = false
	m.publicURL = ""
	if mode == "named" && cfg.Tunnel.CustomDomain != "" {
		m.publicURL = "https://" + strings.TrimPrefix(cfg.Tunnel.CustomDomain, "https://")
	}
	m.lastError = ""
	m.ready = make(chan struct{})
	m.readyOnce = sync.Once{}
	ready := m.ready
	m.mu.Unlock()

	go m.captureTunnelOutput(stdout, logFile)
	go m.captureTunnelOutput(stderr, logFile)
	go m.wait(command, logFile)

	wait := 1500 * time.Millisecond
	if mode == "quick" {
		wait = 10 * time.Second
	}
	select {
	case <-ready:
		return m.validateStartedTunnel(command, mode)
	case <-time.After(wait):
		return m.validateStartedTunnel(command, mode)
	}
}

func tunnelCommandArgs(cfg TautlineConfig, mode, origin string) []string {
	args := []string{"tunnel"}
	if protocol := strings.TrimSpace(cfg.Tunnel.Protocol); protocol != "" {
		args = append(args, "--protocol", protocol)
	}
	args = append(args, "--url", origin, "--http-host-header", "localhost")
	if mode == "named" {
		args = append(args, "run", cfg.Tunnel.Name)
	}
	return args
}

func (m *tunnelManager) validateStartedTunnel(command *exec.Cmd, mode string) error {
	m.mu.Lock()
	running := m.command == command && command.ProcessState == nil
	publicURL := m.publicURL
	lastError := m.lastError
	m.mu.Unlock()
	if !running {
		if lastError == "" {
			lastError = "cloudflared exited before becoming ready"
		}
		return errors.New(lastError)
	}
	if mode == "quick" && publicURL == "" {
		return errors.New("quick tunnel started but no trycloudflare URL was detected")
	}
	return nil
}

func (m *tunnelManager) captureTunnelOutput(reader io.Reader, logFile *os.File) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(logFile, line)
		if match := quickTunnelURLPattern.FindString(line); match != "" {
			m.mu.Lock()
			m.publicURL = match
			m.mu.Unlock()
			m.signalReady()
		}
		if strings.Contains(strings.ToLower(line), "registered tunnel connection") || strings.Contains(strings.ToLower(line), "connection registered") {
			m.signalReady()
		}
	}
}

func (m *tunnelManager) signalReady() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready == nil {
		return
	}
	m.readyOnce.Do(func() { close(m.ready) })
}

func (m *tunnelManager) wait(command *exec.Cmd, logFile *os.File) {
	err := command.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.command == command {
		m.command = nil
	}
	if m.logFile == logFile {
		m.logFile = nil
	}
	_ = logFile.Close()
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "killed") {
		m.lastError = err.Error()
	}
	if m.ready != nil {
		m.readyOnce.Do(func() { close(m.ready) })
	}
}

func (m *tunnelManager) stop() error {
	m.mu.Lock()
	command := m.command
	externalPID := m.externalPID
	externalAdopted := m.externalAdopted
	m.mu.Unlock()

	pid := 0
	if command != nil && command.Process != nil {
		pid = command.Process.Pid
	} else if externalAdopted {
		pid = externalPID
	}
	if pid <= 0 {
		return nil
	}
	killProcessTree(pid)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			m.mu.Lock()
			if m.externalPID == pid {
				m.externalPID = 0
				m.externalAdopted = false
			}
			m.mu.Unlock()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("cloudflared did not stop within 5 seconds")
}

func (m *tunnelManager) ensureNamedTunnel(name string) (string, error) {
	executable, err := m.resolveExecutable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	listCommand := exec.CommandContext(ctx, executable, "tunnel", "list", "--output", "json")
	listCommand.Dir = mustWorkingDirectory()
	output, listErr := listCommand.Output()
	if listErr == nil {
		var items []tunnelListItem
		if json.Unmarshal(output, &items) == nil {
			for _, item := range items {
				if strings.EqualFold(item.Name, name) {
					return item.ID, nil
				}
			}
		}
	}
	createCommand := exec.CommandContext(ctx, executable, "tunnel", "create", name)
	createCommand.Dir = mustWorkingDirectory()
	var combined bytes.Buffer
	createCommand.Stdout = &combined
	createCommand.Stderr = &combined
	if err := createCommand.Run(); err != nil {
		message := strings.TrimSpace(combined.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("create named tunnel: %s", message)
	}
	if id := tunnelUUIDPattern.FindString(combined.String()); id != "" {
		return id, nil
	}
	listCommand = exec.CommandContext(ctx, executable, "tunnel", "list", "--output", "json")
	output, err = listCommand.Output()
	if err == nil {
		var items []tunnelListItem
		if json.Unmarshal(output, &items) == nil {
			for _, item := range items {
				if strings.EqualFold(item.Name, name) {
					return item.ID, nil
				}
			}
		}
	}
	return "", errors.New("named tunnel was created but its UUID could not be resolved")
}

func (m *tunnelManager) routeDNS(domain string) (TunnelStatus, error) {
	domain = strings.TrimSpace(strings.TrimPrefix(domain, "https://"))
	if domain == "" || strings.ContainsAny(domain, "/ \\") {
		return TunnelStatus{}, fmt.Errorf("invalid custom domain %q", domain)
	}
	cfg := m.store.snapshot()
	if strings.TrimSpace(cfg.Tunnel.Name) == "" {
		return TunnelStatus{}, errors.New("configure a named tunnel before generating DNS")
	}
	id, err := m.ensureNamedTunnel(cfg.Tunnel.Name)
	if err != nil {
		return TunnelStatus{}, err
	}
	executable, err := m.resolveExecutable()
	if err != nil {
		return TunnelStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "tunnel", "route", "dns", cfg.Tunnel.Name, domain)
	command.Dir = mustWorkingDirectory()
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return TunnelStatus{}, fmt.Errorf("generate Cloudflare DNS: %s", message)
	}
	if err := m.store.update(func(value *TautlineConfig) error {
		value.Tunnel.Mode = "named"
		value.Tunnel.CustomDomain = domain
		value.PublicBaseURL = "https://" + domain
		return nil
	}); err != nil {
		return TunnelStatus{}, err
	}
	m.mu.Lock()
	m.tunnelID = id
	m.publicURL = "https://" + domain
	m.lastError = ""
	m.mu.Unlock()
	return m.status(), nil
}
