package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultProcessYieldMS      = 10_000
	defaultProcessPollMS       = 5_000
	maxProcessYieldMS          = 30_000
	defaultProcessOutputBytes  = 64 * 1024
	maxProcessOutputBytes      = 128 * 1024
	processSessionBufferBytes  = 1024 * 1024
	completedProcessSessionTTL = 5 * time.Minute
)

func registerProcessSessionTools(s *server.MCPServer) {
	execTool := mcp.NewTool("exec_command",
		mcp.WithTitleAnnotation("Start interactive command"),
		mcp.WithDescription("Start a shell command in an open workspace. If it is still running after the bounded yield window, return a process session ID. Use write_stdin to poll output, send input, close stdin, interrupt, or terminate the same process. Sessions are pipe-backed and workspace-scoped."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to start")),
		mcp.WithString("cwd", mcp.Description("Optional working directory relative to the workspace root")),
		mcp.WithNumber("yield_ms", mcp.Description("Wait before returning, default 10000 and maximum 30000 milliseconds")),
		mcp.WithNumber("max_output_bytes", mcp.Description("Maximum incremental output returned per call, default 65536 and maximum 131072 bytes")),
		mcp.WithOutputSchema[processSessionSnapshot](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(execTool, handleExecCommand)

	stdinTool := mcp.NewTool("write_stdin",
		mcp.WithTitleAnnotation("Interact with command"),
		mcp.WithDescription("Interact with one running process session in the same workspace. Send chars, close stdin, request Ctrl-C or Ctrl-Break, terminate the process tree, or poll for incremental output. Completed sessions are removed after their final output is read."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier that owns the process session")),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Process session identifier returned by exec_command")),
		mcp.WithString("chars", mcp.Description("Optional UTF-8 input to write to the process stdin")),
		mcp.WithBoolean("close_stdin", mcp.Description("Close the process stdin after any supplied chars")),
		mcp.WithBoolean("interrupt", mcp.Description("Send Ctrl-C on Unix or Ctrl-Break to the Windows process group")),
		mcp.WithBoolean("terminate", mcp.Description("Forcefully terminate the process tree")),
		mcp.WithNumber("yield_ms", mcp.Description("Wait for output or completion, default 5000 and maximum 30000 milliseconds")),
		mcp.WithNumber("max_output_bytes", mcp.Description("Maximum incremental output returned per call, default 65536 and maximum 131072 bytes")),
		mcp.WithOutputSchema[processSessionSnapshot](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(stdinTool, handleWriteStdin)
}

func handleExecCommand(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	app, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	workspaceID := argStr(req, "workspace_id")
	_, cwd, err := resolveWorkspaceDirectory(workspaceID, argStr(req, "cwd"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	snapshot, err := app.processes.start(
		workspaceID,
		cwd,
		argStr(req, "command"),
		argInt(req, "yield_ms", defaultProcessYieldMS),
		argInt(req, "max_output_bytes", defaultProcessOutputBytes),
	)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fallback := snapshot.Summary
	if snapshot.Running {
		fallback += ". Continue with write_stdin using session " + snapshot.SessionID
	}
	return newToolResult("exec_command", snapshot, snapshot, fallback), nil
}

func handleWriteStdin(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	app, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	snapshot, err := app.processes.interact(
		argStr(req, "workspace_id"),
		argStr(req, "session_id"),
		argStr(req, "chars"),
		argBool(req, "close_stdin", false),
		argBool(req, "interrupt", false),
		argBool(req, "terminate", false),
		argInt(req, "yield_ms", defaultProcessPollMS),
		argInt(req, "max_output_bytes", defaultProcessOutputBytes),
	)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return newToolResult("write_stdin", snapshot, snapshot, snapshot.Summary), nil
}

type processSessionSnapshot struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
	Command     string `json:"command"`
	SessionID   string `json:"sessionId,omitempty"`
	Output      string `json:"output"`
	Running     bool   `json:"running"`
	Success     bool   `json:"success"`
	ExitCode    *int   `json:"exitCode,omitempty"`
	WallTimeMS  int64  `json:"wallTimeMs"`
	Truncated   bool   `json:"truncated,omitempty"`
	Redacted    bool   `json:"redacted,omitempty"`
}

type processSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*processSession
}

type processSession struct {
	mu          sync.Mutex
	id          string
	workspaceID string
	cwd         string
	command     string
	startedAt   time.Time
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	cancel      context.CancelFunc
	output      *processOutputBuffer
	done        chan struct{}
	running     bool
	exitCode    *int
	waitErr     error
	cleanup     *time.Timer
}

type processOutputBuffer struct {
	mu      sync.Mutex
	data    []byte
	dropped int64
	limit   int
}

func newProcessSessionManager() *processSessionManager {
	return &processSessionManager{sessions: make(map[string]*processSession)}
}

func (m *processSessionManager) start(workspaceID, cwd, command string, yieldMS, maxOutputBytes int) (processSessionSnapshot, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return processSessionSnapshot{}, errors.New("empty command")
	}
	state, err := getWorkspace(workspaceID)
	if err != nil {
		return processSessionSnapshot{}, err
	}

	runContext, cancel := context.WithCancel(context.Background())
	cmd := shellCommand(runContext, command)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"TAUTLINE_WORKSPACE_ID="+workspaceID,
		"TAUTLINE_WORKSPACE_ROOT="+state.Root,
		"NO_COLOR=1",
		"PAGER=cat",
		"GIT_PAGER=cat",
	)
	prepareInteractiveCommand(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return processSessionSnapshot{}, fmt.Errorf("open command stdin: %w", err)
	}
	output := &processOutputBuffer{limit: processSessionBufferBytes}
	cmd.Stdout = output
	cmd.Stderr = output

	session := &processSession{
		id:          "proc_" + randomHex(6),
		workspaceID: workspaceID,
		cwd:         cwd,
		command:     command,
		startedAt:   time.Now().UTC(),
		cmd:         cmd,
		stdin:       stdin,
		cancel:      cancel,
		output:      output,
		done:        make(chan struct{}),
		running:     true,
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return processSessionSnapshot{}, err
	}

	m.mu.Lock()
	m.sessions[session.id] = session
	m.mu.Unlock()
	go m.wait(session)

	yieldMS = boundedProcessValue(yieldMS, defaultProcessYieldMS, maxProcessYieldMS)
	waitForProcessSession(session, time.Duration(yieldMS)*time.Millisecond)
	snapshot := session.snapshot(maxOutputBytes)
	if !snapshot.Running {
		m.remove(session.id)
	}
	return snapshot, nil
}

func (m *processSessionManager) interact(workspaceID, sessionID, chars string, closeStdin, interrupt, terminate bool, yieldMS, maxOutputBytes int) (processSessionSnapshot, error) {
	session, err := m.getOwned(workspaceID, sessionID)
	if err != nil {
		return processSessionSnapshot{}, err
	}

	session.mu.Lock()
	running := session.running
	stdin := session.stdin
	pid := 0
	if session.cmd != nil && session.cmd.Process != nil {
		pid = session.cmd.Process.Pid
	}
	session.mu.Unlock()

	if chars != "" {
		if !running {
			return processSessionSnapshot{}, fmt.Errorf("process session %s has already finished", session.id)
		}
		if _, err := io.WriteString(stdin, chars); err != nil {
			return processSessionSnapshot{}, fmt.Errorf("write process stdin: %w", err)
		}
	}
	if closeStdin {
		if err := stdin.Close(); err != nil && running {
			return processSessionSnapshot{}, fmt.Errorf("close process stdin: %w", err)
		}
	}
	if interrupt && running {
		if pid <= 0 {
			return processSessionSnapshot{}, errors.New("process session has no active process")
		}
		if err := interruptManagedProcess(pid); err != nil {
			return processSessionSnapshot{}, fmt.Errorf("interrupt process session: %w", err)
		}
	}
	if terminate && running {
		if pid <= 0 {
			return processSessionSnapshot{}, errors.New("process session has no active process")
		}
		if err := forceManagedProcess(pid); err != nil {
			return processSessionSnapshot{}, fmt.Errorf("terminate process session: %w", err)
		}
	}

	if running {
		yieldMS = boundedProcessValue(yieldMS, defaultProcessPollMS, maxProcessYieldMS)
		waitForProcessSession(session, time.Duration(yieldMS)*time.Millisecond)
	}
	snapshot := session.snapshot(maxOutputBytes)
	if !snapshot.Running {
		m.remove(session.id)
	}
	return snapshot, nil
}

func (m *processSessionManager) wait(session *processSession) {
	waitErr := session.cmd.Wait()
	exitCode := processExitCode(waitErr)

	session.mu.Lock()
	if !session.running {
		session.mu.Unlock()
		return
	}
	session.running = false
	session.waitErr = waitErr
	session.exitCode = &exitCode
	_ = session.stdin.Close()
	session.cancel()
	close(session.done)
	session.cleanup = time.AfterFunc(completedProcessSessionTTL, func() {
		m.remove(session.id)
	})
	session.mu.Unlock()
}

func (m *processSessionManager) getOwned(workspaceID, sessionID string) (*processSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}
	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("unknown process session %q", sessionID)
	}
	if session.workspaceID != strings.TrimSpace(workspaceID) {
		return nil, fmt.Errorf("process session %s belongs to another workspace", sessionID)
	}
	return session, nil
}

func (m *processSessionManager) remove(sessionID string) {
	m.mu.Lock()
	session := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if session != nil {
		session.mu.Lock()
		if session.cleanup != nil {
			session.cleanup.Stop()
			session.cleanup = nil
		}
		session.mu.Unlock()
	}
}

func (m *processSessionManager) shutdown() {
	m.mu.RLock()
	sessions := make([]*processSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.RUnlock()
	for _, session := range sessions {
		session.mu.Lock()
		pid := 0
		if session.running && session.cmd != nil && session.cmd.Process != nil {
			pid = session.cmd.Process.Pid
		}
		session.mu.Unlock()
		if pid > 0 {
			_ = forceManagedProcess(pid)
		}
	}
}

func (session *processSession) snapshot(maxOutputBytes int) processSessionSnapshot {
	maxOutputBytes = boundedProcessValue(maxOutputBytes, defaultProcessOutputBytes, maxProcessOutputBytes)
	output, truncated, redacted := session.output.drain(maxOutputBytes)

	session.mu.Lock()
	running := session.running
	var exitCode *int
	if session.exitCode != nil {
		value := *session.exitCode
		exitCode = &value
	}
	wallTime := time.Since(session.startedAt).Milliseconds()
	cwd := session.cwd
	command := session.command
	workspaceID := session.workspaceID
	sessionID := session.id
	session.mu.Unlock()

	path := cwd
	if state, err := getWorkspace(workspaceID); err == nil {
		if relative, relativeErr := state.relativePath(cwd); relativeErr == nil && relative != "" {
			path = relative
		} else if filepath.Clean(cwd) == filepath.Clean(state.Root) {
			path = "."
		}
	}
	title := "Command running"
	summary := fmt.Sprintf("Running · %dms", wallTime)
	success := false
	if !running {
		title = "Command finished"
		code := -1
		if exitCode != nil {
			code = *exitCode
		}
		summary = fmt.Sprintf("Exit %d · %dms", code, wallTime)
		success = code == 0
		sessionID = ""
	}
	return processSessionSnapshot{
		Kind:        "command",
		Title:       title,
		Summary:     summary,
		WorkspaceID: workspaceID,
		Path:        path,
		Command:     command,
		SessionID:   sessionID,
		Output:      output,
		Running:     running,
		Success:     success,
		ExitCode:    exitCode,
		WallTimeMS:  wallTime,
		Truncated:   truncated,
		Redacted:    redacted,
	}
}

func (buffer *processOutputBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.limit < 1 {
		buffer.limit = processSessionBufferBytes
	}
	originalLength := len(value)
	if len(value) >= buffer.limit {
		buffer.dropped += int64(len(buffer.data) + len(value) - buffer.limit)
		buffer.data = append(buffer.data[:0], value[len(value)-buffer.limit:]...)
		return originalLength, nil
	}
	if len(buffer.data)+len(value) > buffer.limit {
		drop := len(buffer.data) + len(value) - buffer.limit
		buffer.dropped += int64(drop)
		copy(buffer.data, buffer.data[drop:])
		buffer.data = buffer.data[:len(buffer.data)-drop]
	}
	buffer.data = append(buffer.data, value...)
	return originalLength, nil
}

func (buffer *processOutputBuffer) drain(limit int) (string, bool, bool) {
	buffer.mu.Lock()
	raw := append([]byte(nil), buffer.data...)
	dropped := buffer.dropped
	buffer.data = buffer.data[:0]
	buffer.dropped = 0
	buffer.mu.Unlock()

	redacted, changed := redactSensitiveText(string(raw))
	if dropped > 0 {
		redacted = fmt.Sprintf("[process output truncated: %d earlier bytes omitted]\n%s", dropped, redacted)
	}
	trimmed, truncated := truncateUTF8(redacted, limit)
	return strings.TrimRight(trimmed, "\r\n"), dropped > 0 || truncated, changed
}

func waitForProcessSession(session *processSession, duration time.Duration) {
	if duration <= 0 {
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-session.done:
	case <-timer.C:
	}
}

func boundedProcessValue(value, fallback, maximum int) int {
	if value <= 0 {
		value = fallback
	}
	if value > maximum {
		value = maximum
	}
	return value
}

func processExitCode(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(waitErr, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}
