package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const maxBrowserOutputBytes = 512 * 1024

type LightpandaStatus struct {
	Available        bool   `json:"available"`
	Detecting        bool   `json:"detecting"`
	Starting         bool   `json:"starting"`
	Running          bool   `json:"running"`
	Mode             string `json:"mode,omitempty"`
	Executable       string `json:"executable,omitempty"`
	DockerImage      string `json:"docker_image,omitempty"`
	Endpoint         string `json:"endpoint"`
	PID              int    `json:"pid,omitempty"`
	NativeMCPReady   bool   `json:"native_mcp_ready"`
	NativeMCPTools   int    `json:"native_mcp_tools"`
	NativeMCPServer  string `json:"native_mcp_server,omitempty"`
	NativeMCPVersion string `json:"native_mcp_version,omitempty"`
	NativeMCPError   string `json:"native_mcp_error,omitempty"`
	LastError        string `json:"last_error,omitempty"`
}

type BrowserFetchResult struct {
	URL       string `json:"url"`
	HTML      string `json:"html"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Duration  int64  `json:"duration_ms"`
}

type lightpandaRunner struct {
	mode        string
	executable  string
	dockerImage string
	container   string
}

type lightpandaManager struct {
	mu             sync.Mutex
	store          *configStore
	command        *exec.Cmd
	logFile        *os.File
	runner         lightpandaRunner
	detectedRunner lightpandaRunner
	detectionError string
	detecting      bool
	starting       bool
	lastError      string

	nativeInitMu        sync.Mutex
	nativeMu            sync.RWMutex
	nativeClient        *mcpclient.Client
	nativeTools         []mcp.Tool
	nativeServerName    string
	nativeServerVersion string
	nativeLastError     string
}

func newLightpandaManager(store *configStore) *lightpandaManager {
	return &lightpandaManager{store: store}
}

func (m *lightpandaManager) resolveRunner() (lightpandaRunner, error) {
	cfg := m.store.snapshot().Lightpanda
	configured := strings.TrimSpace(cfg.Executable)
	mode := strings.ToLower(configured)
	container := fmt.Sprintf("tautline-lightpanda-%d", cfg.Port)

	if mode == "docker" {
		docker, err := findDockerCLI()
		if err != nil {
			return lightpandaRunner{}, err
		}
		return lightpandaRunner{mode: "docker", executable: docker, dockerImage: cfg.DockerImage, container: container}, nil
	}
	if mode == "wsl" {
		wsl, err := findWSLLightpanda()
		if err != nil {
			return lightpandaRunner{}, err
		}
		return lightpandaRunner{mode: "wsl", executable: wsl}, nil
	}
	if configured != "" && mode != "auto" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return lightpandaRunner{}, err
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			return lightpandaRunner{}, fmt.Errorf("Lightpanda binary %s was not found", absolute)
		}
		return lightpandaRunner{mode: "binary", executable: absolute}, nil
	}

	candidates := []string{filepath.Join("bin", executableName("lightpanda"))}
	if located, err := exec.LookPath(executableName("lightpanda")); err == nil {
		candidates = append(candidates, located)
	}
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
			return lightpandaRunner{mode: "binary", executable: absolute}, nil
		}
	}
	if docker, err := findDockerCLI(); err == nil {
		return lightpandaRunner{mode: "docker", executable: docker, dockerImage: cfg.DockerImage, container: container}, nil
	}
	if wsl, err := findWSLLightpanda(); err == nil {
		return lightpandaRunner{mode: "wsl", executable: wsl}, nil
	}
	if runtime.GOOS == "windows" {
		return lightpandaRunner{}, errors.New("Lightpanda has no native Windows binary; install or start Docker Desktop, or install Lightpanda in WSL2")
	}
	return lightpandaRunner{}, errors.New("Lightpanda was not found; install the binary or set TAUTLINE_LIGHTPANDA_PATH")
}

func findDockerCLI() (string, error) {
	for _, name := range []string{"docker.exe", "docker"} {
		located, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		command := exec.CommandContext(ctx, located, "info", "--format", "{{.ServerVersion}}")
		output, probeErr := command.Output()
		cancel()
		if probeErr == nil && strings.TrimSpace(string(output)) != "" {
			return located, nil
		}
	}
	return "", errors.New("Docker Desktop is not running or the Docker CLI was not found")
}

func findWSLLightpanda() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("WSL mode is available only on Windows")
	}
	wsl, err := exec.LookPath("wsl.exe")
	if err != nil {
		return "", errors.New("WSL2 was not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, wsl, "sh", "-lc", "command -v lightpanda")
	if output, err := command.Output(); err != nil || strings.TrimSpace(string(output)) == "" {
		return "", errors.New("Lightpanda is not installed in the default WSL2 distribution")
	}
	return wsl, nil
}

func (m *lightpandaManager) status() LightpandaStatus {
	cfg := m.store.snapshot().Lightpanda
	m.mu.Lock()
	starting := m.starting
	detecting := m.detecting
	command := m.command
	activeRunner := m.runner
	detectedRunner := m.detectedRunner
	detectionError := m.detectionError
	lastError := m.lastError
	m.mu.Unlock()

	if lastError == "" {
		lastError = detectionError
	}
	nativeReady, nativeToolCount, nativeServerName, nativeServerVersion, nativeError := m.nativeMCPStatus()
	status := LightpandaStatus{
		Detecting:        detecting,
		Starting:         starting,
		Endpoint:         fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port),
		NativeMCPReady:   nativeReady,
		NativeMCPTools:   nativeToolCount,
		NativeMCPServer:  nativeServerName,
		NativeMCPVersion: nativeServerVersion,
		NativeMCPError:   nativeError,
		LastError:        lastError,
		DockerImage:      cfg.DockerImage,
	}
	if detectedRunner.mode != "" {
		status.Available = true
		status.Mode = detectedRunner.mode
		status.Executable = detectedRunner.executable
		if detectedRunner.mode != "docker" {
			status.DockerImage = ""
		}
	}
	if command != nil && command.Process != nil && command.ProcessState == nil {
		status.Available = true
		status.Running = true
		status.PID = command.Process.Pid
		if activeRunner.mode != "" {
			status.Mode = activeRunner.mode
			status.Executable = activeRunner.executable
		}
		return status
	}
	connection, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), 75*time.Millisecond)
	if err == nil {
		status.Available = true
		status.Running = true
		_ = connection.Close()
	}
	return status
}

func (m *lightpandaManager) probeRunner() {
	m.mu.Lock()
	if m.detecting || m.starting {
		m.mu.Unlock()
		return
	}
	m.detecting = true
	m.mu.Unlock()

	runner, err := m.resolveRunner()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detecting = false
	if err != nil {
		m.detectedRunner = lightpandaRunner{}
		m.detectionError = err.Error()
		return
	}
	m.detectedRunner = runner
	m.detectionError = ""
}

func (m *lightpandaManager) start() error {
	m.mu.Lock()
	if m.command != nil && m.command.Process != nil && m.command.ProcessState == nil {
		m.mu.Unlock()
		return nil
	}
	if m.starting {
		m.mu.Unlock()
		return errors.New("Lightpanda start is already in progress")
	}
	m.starting = true
	m.lastError = ""
	m.mu.Unlock()

	completed := false
	defer func() {
		if completed {
			return
		}
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()

	cfg := m.store.snapshot()
	address := net.JoinHostPort(cfg.Lightpanda.Host, strconv.Itoa(cfg.Lightpanda.Port))
	if connection, dialErr := net.DialTimeout("tcp", address, 250*time.Millisecond); dialErr == nil {
		_ = connection.Close()
		m.finishLightpandaStart(nil)
		completed = true
		return nil
	}
	runner, err := m.resolveRunner()
	if err != nil {
		m.finishLightpandaStart(err)
		completed = true
		return err
	}
	m.mu.Lock()
	m.detectedRunner = runner
	m.detectionError = ""
	m.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(cfg.RuntimeDir, "logs"), 0o700); err != nil {
		m.finishLightpandaStart(err)
		completed = true
		return err
	}
	logPath := filepath.Join(cfg.RuntimeDir, "logs", "lightpanda.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		m.finishLightpandaStart(err)
		completed = true
		return err
	}
	command := lightpandaServeCommand(runner, cfg.Lightpanda)
	command.Dir = mustWorkingDirectory()
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		m.finishLightpandaStart(err)
		completed = true
		return err
	}

	m.mu.Lock()
	m.command = command
	m.logFile = logFile
	m.runner = runner
	m.mu.Unlock()
	go m.wait(command, logFile)

	deadlineDuration := 8 * time.Second
	if runner.mode == "docker" {
		deadlineDuration = 90 * time.Second
	}
	deadline := time.Now().Add(deadlineDuration)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			m.finishLightpandaStart(nil)
			completed = true
			return nil
		}
		if command.ProcessState != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if command.ProcessState != nil {
		err = fmt.Errorf("Lightpanda %s runner exited before CDP became ready; inspect %s", runner.mode, logPath)
	} else {
		err = fmt.Errorf("Lightpanda started but CDP endpoint %s did not become ready", address)
	}
	m.finishLightpandaStart(err)
	completed = true
	return err
}

func (m *lightpandaManager) finishLightpandaStart(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starting = false
	if err == nil {
		m.lastError = ""
		return
	}
	m.lastError = err.Error()
}

func lightpandaServeCommand(runner lightpandaRunner, cfg LightpandaConfig) *exec.Cmd {
	browserArgs := []string{"serve", "--host", cfg.Host, "--port", fmt.Sprintf("%d", cfg.Port), "--log-level", "warn"}
	if cfg.ObeyRobots {
		browserArgs = append(browserArgs, "--obey-robots")
	}
	switch runner.mode {
	case "docker":
		args := []string{
			"run", "--rm", "--name", runner.container,
			"-p", fmt.Sprintf("%s:%d:%d", cfg.Host, cfg.Port, cfg.Port),
			runner.dockerImage,
			"serve", "--host", "0.0.0.0", "--port", fmt.Sprintf("%d", cfg.Port), "--log-level", "warn",
		}
		if cfg.ObeyRobots {
			args = append(args, "--obey-robots")
		}
		return exec.Command(runner.executable, args...)
	case "wsl":
		return exec.Command(runner.executable, "sh", "-lc", "exec lightpanda "+shellJoin(browserArgs))
	default:
		return exec.Command(runner.executable, browserArgs...)
	}
}

func (m *lightpandaManager) wait(command *exec.Cmd, logFile *os.File) {
	err := command.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.command == command {
		m.command = nil
		m.runner = lightpandaRunner{}
	}
	if m.logFile == logFile {
		m.logFile = nil
	}
	_ = logFile.Close()
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "killed") {
		m.lastError = err.Error()
	}
}

func (m *lightpandaManager) stop() error {
	m.mu.Lock()
	command := m.command
	runner := m.runner
	m.mu.Unlock()
	if command == nil || command.Process == nil {
		return nil
	}
	if runner.mode == "docker" && runner.container != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = exec.CommandContext(ctx, runner.executable, "stop", "--time", "2", runner.container).Run()
		cancel()
	}
	killProcessTree(command.Process.Pid)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		running := m.command == command
		m.mu.Unlock()
		if !running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Lightpanda did not stop within 5 seconds")
}

func (m *lightpandaManager) fetch(ctx context.Context, rawURL string) (BrowserFetchResult, error) {
	started := time.Now()
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return BrowserFetchResult{}, fmt.Errorf("invalid browser URL %q", rawURL)
	}
	runner, err := m.resolveRunner()
	if err != nil {
		return BrowserFetchResult{}, err
	}
	cfg := m.store.snapshot().Lightpanda
	browserArgs := []string{"fetch"}
	if cfg.ObeyRobots {
		browserArgs = append(browserArgs, "--obey-robots")
	}
	browserArgs = append(browserArgs, "--dump", "html", "--log-level", "error", parsed.String())
	command := lightpandaFetchCommand(ctx, runner, browserArgs)
	command.Dir = mustWorkingDirectory()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &limitedWriter{writer: &stdout, remaining: maxBrowserOutputBytes}
	command.Stderr = &limitedWriter{writer: &stderr, remaining: 128 * 1024}
	err = command.Run()
	if ctx.Err() != nil {
		return BrowserFetchResult{}, ctx.Err()
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return BrowserFetchResult{}, fmt.Errorf("Lightpanda fetch failed: %s", detail)
	}
	html := stdout.String()
	return BrowserFetchResult{
		URL:       parsed.String(),
		HTML:      html,
		Bytes:     len(html),
		Truncated: len(html) >= maxBrowserOutputBytes,
		Duration:  time.Since(started).Milliseconds(),
	}, nil
}

func lightpandaFetchCommand(ctx context.Context, runner lightpandaRunner, browserArgs []string) *exec.Cmd {
	switch runner.mode {
	case "docker":
		args := append([]string{"run", "--rm", runner.dockerImage}, browserArgs...)
		return exec.CommandContext(ctx, runner.executable, args...)
	case "wsl":
		return exec.CommandContext(ctx, runner.executable, "sh", "-lc", "exec lightpanda "+shellJoin(browserArgs))
	default:
		return exec.CommandContext(ctx, runner.executable, browserArgs...)
	}
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	originalLength := len(data)
	if w.remaining <= 0 {
		return originalLength, nil
	}
	toWrite := data
	if len(toWrite) > w.remaining {
		toWrite = toWrite[:w.remaining]
	}
	written, err := w.writer.Write(toWrite)
	w.remaining -= written
	if err != nil {
		return written, err
	}
	return originalLength, nil
}
