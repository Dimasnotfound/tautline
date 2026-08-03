package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxAgentRuns            = 100
	maxAgentReadBytes       = 128 * 1024
	maxAgentSearchMatches   = 20
	maxAgentImageDataBytes  = 12 * 1024 * 1024
	maxAgentOutputPreview   = 16 * 1024
	maxAgentToolOutputBytes = 96 * 1024
)

type AgentSlot struct {
	ID          string `json:"id"`
	Enabled     bool   `json:"enabled"`
	AllowImages bool   `json:"allow_images"`
	RTK         bool   `json:"rtk"`
	Caveman     bool   `json:"caveman"`
	Busy        bool   `json:"busy"`
	ActiveRunID string `json:"active_run_id,omitempty"`
}

type AgentRun struct {
	ID              string         `json:"id"`
	SlotID          string         `json:"slot_id"`
	AgentID         string         `json:"agent_id,omitempty"`
	Name            string         `json:"name,omitempty"`
	Role            string         `json:"role,omitempty"`
	Provider        string         `json:"provider"`
	Model           string         `json:"model"`
	Task            string         `json:"task"`
	WorkspaceID     string         `json:"workspace_id,omitempty"`
	RequiresImages  bool           `json:"requires_images"`
	ImageCapability string         `json:"image_capability"`
	RTK             bool           `json:"rtk"`
	Caveman         bool           `json:"caveman"`
	Status          string         `json:"status"`
	Phase           string         `json:"phase"`
	Activity        string         `json:"activity"`
	Output          string         `json:"output,omitempty"`
	Error           string         `json:"error,omitempty"`
	Usage           map[string]any `json:"usage,omitempty"`
	StartedAt       time.Time      `json:"started_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
}

type AgentDelegateRequest struct {
	Task                string
	WorkspaceID         string
	AgentID             string
	Name                string
	Role                string
	Provider            string
	Model               string
	TimeoutSeconds      int
	RequiresImages      bool
	ModelSupportsImages bool
	ImageDataURL        string
}

type agentRunState struct {
	value  AgentRun
	cancel context.CancelFunc
}

type agentManager struct {
	mu           sync.RWMutex
	store        *configStore
	router       *routerClient
	lightpanda   *lightpandaManager
	slots        []AgentSlot
	runs         map[string]*agentRunState
	history      []string
	statePath    string
	routerStatus RouterStatus
}

func newAgentManager(store *configStore, router *routerClient, lightpanda *lightpandaManager) (*agentManager, error) {
	cfg := store.snapshot()
	manager := &agentManager{
		store:      store,
		router:     router,
		lightpanda: lightpanda,
		runs:       make(map[string]*agentRunState),
		statePath:  filepath.Join(cfg.RuntimeDir, "state", "agents.json"),
	}
	if data, err := os.ReadFile(manager.statePath); err == nil {
		_ = json.Unmarshal(data, &manager.slots)
	}
	if len(manager.slots) == 0 {
		for index := 0; index < cfg.AgentCapacity; index++ {
			manager.slots = append(manager.slots, manager.defaultSlot(index+1))
		}
	}
	manager.normalizeSlots()
	if err := manager.persistSlots(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *agentManager) defaultSlot(number int) AgentSlot {
	cfg := m.store.snapshot()
	return AgentSlot{
		ID:          fmt.Sprintf("slot-%02d", number),
		Enabled:     true,
		AllowImages: cfg.DefaultImageGate,
		RTK:         cfg.DefaultRTK,
		Caveman:     cfg.DefaultCaveman,
	}
}

func (m *agentManager) normalizeSlots() {
	seen := make(map[string]bool)
	for index := range m.slots {
		m.slots[index].Busy = false
		m.slots[index].ActiveRunID = ""
		if strings.TrimSpace(m.slots[index].ID) == "" || seen[m.slots[index].ID] {
			m.slots[index].ID = fmt.Sprintf("slot-%02d", index+1)
		}
		seen[m.slots[index].ID] = true
	}
	if len(m.slots) > maximumAgentCapacity {
		m.slots = m.slots[:maximumAgentCapacity]
	}
}

func (m *agentManager) persistSlots() error {
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o700); err != nil {
		return err
	}
	persisted := make([]AgentSlot, len(m.slots))
	copy(persisted, m.slots)
	for index := range persisted {
		persisted[index].Busy = false
		persisted[index].ActiveRunID = ""
	}
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := m.statePath + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, m.statePath)
}

func (m *agentManager) slotsSnapshot() []AgentSlot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]AgentSlot, len(m.slots))
	copy(result, m.slots)
	return result
}

func (m *agentManager) runsSnapshot() []AgentRun {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]AgentRun, 0, len(m.history))
	for index := len(m.history) - 1; index >= 0; index-- {
		if state := m.runs[m.history[index]]; state != nil {
			copyValue := state.value
			copyValue.Usage = cloneAnyMap(state.value.Usage)
			result = append(result, copyValue)
		}
	}
	return result
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (m *agentManager) addSlot() (AgentSlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.slots) >= maximumAgentCapacity {
		return AgentSlot{}, fmt.Errorf("maximum sub-agent capacity is %d", maximumAgentCapacity)
	}
	used := make(map[string]bool)
	for _, slot := range m.slots {
		used[slot.ID] = true
	}
	number := 1
	for used[fmt.Sprintf("slot-%02d", number)] {
		number++
	}
	slot := m.defaultSlot(number)
	m.slots = append(m.slots, slot)
	if err := m.persistSlots(); err != nil {
		m.slots = m.slots[:len(m.slots)-1]
		return AgentSlot{}, err
	}
	return slot, nil
}

func (m *agentManager) removeSlot(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.slots) <= 1 {
		return errors.New("at least one sub-agent slot must remain")
	}
	for index, slot := range m.slots {
		if slot.ID != id {
			continue
		}
		if slot.Busy {
			return fmt.Errorf("sub-agent %s is busy", id)
		}
		m.slots = append(m.slots[:index], m.slots[index+1:]...)
		return m.persistSlots()
	}
	return fmt.Errorf("unknown sub-agent slot %q", id)
}

func (m *agentManager) updateSlot(id string, enabled, allowImages, rtk, caveman *bool) (AgentSlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index := range m.slots {
		if m.slots[index].ID != id {
			continue
		}
		if enabled != nil {
			if m.slots[index].Busy && !*enabled {
				return AgentSlot{}, fmt.Errorf("sub-agent %s is busy", id)
			}
			m.slots[index].Enabled = *enabled
		}
		if allowImages != nil {
			m.slots[index].AllowImages = *allowImages
		}
		if rtk != nil {
			m.slots[index].RTK = *rtk
		}
		if caveman != nil {
			m.slots[index].Caveman = *caveman
		}
		if err := m.persistSlots(); err != nil {
			return AgentSlot{}, err
		}
		return m.slots[index], nil
	}
	return AgentSlot{}, fmt.Errorf("unknown sub-agent slot %q", id)
}

func (m *agentManager) delegate(request AgentDelegateRequest) (AgentRun, error) {
	request.Task = strings.TrimSpace(request.Task)
	if request.Task == "" {
		return AgentRun{}, errors.New("task is required")
	}
	if len(request.ImageDataURL) > maxAgentImageDataBytes {
		return AgentRun{}, fmt.Errorf("image payload exceeds %d bytes", maxAgentImageDataBytes)
	}
	if request.RequiresImages {
		if !request.ModelSupportsImages {
			return AgentRun{}, errors.New("image task rejected: model_supports_images must be explicitly true")
		}
		if request.ImageDataURL == "" {
			return AgentRun{}, errors.New("image task requires image_data_url")
		}
		if !strings.HasPrefix(request.ImageDataURL, "data:image/") {
			return AgentRun{}, errors.New("image_data_url must be an in-memory data:image URL")
		}
	}
	cfg := m.store.snapshot()
	if !cfg.AgentEnabled {
		return AgentRun{}, errors.New("sub-agent delegation is disabled")
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = cfg.Router.DefaultModel
	}
	if !modelAllowed(model, cfg.Router.AllowedModels) {
		return AgentRun{}, fmt.Errorf("model %q is not allowed; allowed models: %s", model, strings.Join(cfg.Router.AllowedModels, ", "))
	}

	m.mu.Lock()
	slotIndex := -1
	for index := range m.slots {
		slot := m.slots[index]
		if !slot.Enabled || slot.Busy {
			continue
		}
		if request.RequiresImages && !slot.AllowImages {
			continue
		}
		slotIndex = index
		break
	}
	if slotIndex < 0 {
		m.mu.Unlock()
		if request.RequiresImages {
			return AgentRun{}, errors.New("no enabled image-capable sub-agent slot is available")
		}
		return AgentRun{}, errors.New("no enabled sub-agent slot is available")
	}
	timeout := request.TimeoutSeconds
	if timeout <= 0 {
		timeout = cfg.AgentTimeout
	}
	if timeout < 30 {
		timeout = 30
	}
	if timeout > 3600 {
		timeout = 3600
	}
	provider := strings.TrimSpace(request.Provider)
	if provider == "" {
		provider = "9Router"
	}
	runID := "run_" + randToken()[:16]
	now := time.Now().UTC()
	slot := &m.slots[slotIndex]
	run := AgentRun{
		ID:              runID,
		SlotID:          slot.ID,
		AgentID:         strings.TrimSpace(request.AgentID),
		Name:            strings.TrimSpace(request.Name),
		Role:            strings.TrimSpace(request.Role),
		Provider:        provider,
		Model:           model,
		Task:            request.Task,
		WorkspaceID:     strings.TrimSpace(request.WorkspaceID),
		RequiresImages:  request.RequiresImages,
		ImageCapability: imageCapabilityLabel(request.RequiresImages, request.ModelSupportsImages),
		RTK:             slot.RTK,
		Caveman:         slot.Caveman,
		Status:          "queued",
		Phase:           "queued",
		Activity:        "Waiting for the 9Router request to start",
		StartedAt:       now,
		UpdatedAt:       now,
		TimeoutSeconds:  timeout,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	m.runs[runID] = &agentRunState{value: run, cancel: cancel}
	m.history = append(m.history, runID)
	slot.Busy = true
	slot.ActiveRunID = runID
	m.trimRunsLocked()
	m.mu.Unlock()

	imageDataURL := request.ImageDataURL
	request.ImageDataURL = ""
	go m.execute(ctx, runID, request, imageDataURL)
	return run, nil
}

func imageCapabilityLabel(requires, supports bool) string {
	if !requires {
		return "not required"
	}
	if supports {
		return "verified"
	}
	return "unsupported"
}

func (m *agentManager) trimRunsLocked() {
	for len(m.history) > maxAgentRuns {
		oldest := m.history[0]
		m.history = m.history[1:]
		if state := m.runs[oldest]; state != nil && (state.value.Status == "running" || state.value.Status == "queued") {
			m.history = append(m.history, oldest)
			break
		}
		delete(m.runs, oldest)
	}
}

func (m *agentManager) execute(ctx context.Context, runID string, request AgentDelegateRequest, imageDataURL string) {
	defer func() { imageDataURL = "" }()
	m.updateRun(runID, func(run *AgentRun) {
		run.Status = "running"
		run.Phase = "preparing"
		run.Activity = "ChatGPT assigned identity, role, model, and task to this slot"
	})
	messages := []routerMessage{{Role: "system", Content: m.systemPrompt(runID)}}
	userContent := any(request.Task)
	if request.RequiresImages {
		userContent = []map[string]any{
			{"type": "text", "text": request.Task},
			{"type": "image_url", "image_url": map[string]any{"url": imageDataURL}},
		}
	}
	messages = append(messages, routerMessage{Role: "user", Content: userContent})

	for round := 1; ; round++ {
		m.updateRun(runID, func(run *AgentRun) {
			run.Phase = "reasoning"
			run.Activity = fmt.Sprintf("Requesting %s through 9Router, round %d", run.Model, round)
		})
		run, ok := m.getRun(runID)
		if !ok {
			return
		}
		response, err := m.router.complete(ctx, routerChatRequest{
			Model:       run.Model,
			Messages:    messages,
			Tools:       m.agentTools(run.WorkspaceID),
			ToolChoice:  "auto",
			Temperature: 0.1,
		}, run.RTK, run.Caveman)
		if err != nil {
			m.finishRun(runID, "failed", "9Router request failed", "", err)
			return
		}
		if response.Model != "" {
			allowed := m.store.snapshot().Router.AllowedModels
			if !modelAllowed(response.Model, allowed) {
				m.finishRun(runID, "failed", "9Router returned a model outside the allowlist", "", fmt.Errorf("model %q is not allowed", response.Model))
				return
			}
		}
		choice := response.Choices[0]
		assistant := choice.Message
		messages = append(messages, assistant)
		if len(assistant.ToolCalls) == 0 {
			output := extractRouterText(assistant.Content)
			if strings.TrimSpace(output) == "" {
				output = "Sub-agent completed without text output."
			}
			m.updateRun(runID, func(run *AgentRun) {
				run.Usage = cloneAnyMap(response.Usage)
				if response.Model != "" {
					run.Model = response.Model
				}
			})
			m.finishRun(runID, "completed", "Task completed", output, nil)
			return
		}
		for _, call := range assistant.ToolCalls {
			m.updateRun(runID, func(run *AgentRun) {
				run.Phase = "tool"
				run.Activity = "Using " + call.Function.Name
			})
			result, toolErr := m.executeTool(ctx, runID, call.Function.Name, call.Function.Arguments)
			if toolErr != nil {
				result = "Tool error: " + toolErr.Error()
			}
			if run.RTK {
				result = compactAgentToolOutput(result)
			}
			messages = append(messages, routerMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    boundString(result, maxAgentToolOutputBytes),
			})
		}
	}
}

func (m *agentManager) systemPrompt(runID string) string {
	run, _ := m.getRun(runID)
	identity := "You are a temporary sub-agent delegated by ChatGPT through Tautline."
	if run.Name != "" {
		identity += " Your assigned name is " + run.Name + "."
	}
	if run.Role != "" {
		identity += " Your assigned role is: " + run.Role + "."
	}
	identity += " Work only on the delegated task. Never claim an action you did not verify. Use the provided read-only tools when evidence is needed. Do not request or expose secrets."
	if run.Caveman {
		identity += " Caveman mode is enabled: use plain direct language, short steps, and avoid ornamental explanation."
	}
	if run.RTK {
		identity += " RTK mode is enabled: keep tool use and final output compact while preserving exact facts, errors, paths, and test results."
	}
	return identity
}

func (m *agentManager) agentTools(workspaceID string) []routerTool {
	tools := []routerTool{newRouterTool(
		"lightpanda_fetch",
		"Fetch and render one public HTTP or HTTPS page using Lightpanda only. Use this when JavaScript-rendered web content is required.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "Absolute HTTP or HTTPS URL"},
			},
			"required": []string{"url"},
		},
	)}
	if workspaceID != "" {
		tools = append(tools,
			newRouterTool("workspace_read", "Read one UTF-8 file from the delegated workspace. Read-only.", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Path relative to the delegated workspace"},
					"max_lines": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
				},
				"required": []string{"path"},
			}),
			newRouterTool("workspace_search", "Search text or a Go regular expression in UTF-8 workspace files. Read-only.", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":          map[string]any{"type": "string"},
					"glob":           map[string]any{"type": "string"},
					"regex":          map[string]any{"type": "boolean"},
					"case_sensitive": map[string]any{"type": "boolean"},
				},
				"required": []string{"query"},
			}),
		)
	}
	return tools
}

func newRouterTool(name, description string, parameters map[string]any) routerTool {
	tool := routerTool{Type: "function"}
	tool.Function.Name = name
	tool.Function.Description = description
	tool.Function.Parameters = parameters
	return tool
}

func (m *agentManager) executeTool(ctx context.Context, runID, name, arguments string) (string, error) {
	run, ok := m.getRun(runID)
	if !ok {
		return "", errors.New("agent run no longer exists")
	}
	var args map[string]any
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return "", fmt.Errorf("invalid tool arguments: %w", err)
		}
	}
	switch name {
	case "lightpanda_fetch":
		result, err := m.lightpanda.fetch(ctx, stringValue(args["url"]))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("URL: %s\nBytes: %d\nTruncated: %t\nDuration: %dms\n\n%s", result.URL, result.Bytes, result.Truncated, result.Duration, result.HTML), nil
	case "workspace_read":
		return readAgentWorkspaceFile(run.WorkspaceID, stringValue(args["path"]), intValue(args["max_lines"], 300))
	case "workspace_search":
		return searchAgentWorkspace(run.WorkspaceID, stringValue(args["query"]), stringValue(args["glob"]), boolValue(args["regex"]), boolValue(args["case_sensitive"]))
	default:
		return "", fmt.Errorf("unsupported sub-agent tool %q", name)
	}
}

func readAgentWorkspaceFile(workspaceID, relativePath string, maxLines int) (string, error) {
	_, path, err := resolveWorkspacePath(workspaceID, relativePath)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAgentReadBytes+1))
	if err != nil {
		return "", err
	}
	truncatedBytes := len(data) > maxAgentReadBytes
	if truncatedBytes {
		data = data[:maxAgentReadBytes]
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", errors.New("file is not UTF-8 text")
	}
	if maxLines < 1 {
		maxLines = 300
	}
	if maxLines > 1000 {
		maxLines = 1000
	}
	lines := strings.Split(string(data), "\n")
	truncatedLines := len(lines) > maxLines
	if truncatedLines {
		lines = lines[:maxLines]
	}
	var builder strings.Builder
	for index, line := range lines {
		fmt.Fprintf(&builder, "%4d  %s\n", index+1, line)
	}
	if truncatedBytes || truncatedLines {
		builder.WriteString("\n[output truncated]\n")
	}
	return builder.String(), nil
}

func searchAgentWorkspace(workspaceID, query, glob string, useRegex, caseSensitive bool) (string, error) {
	state, err := getWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(query) == "" {
		return "", errors.New("query is required")
	}
	matcher, err := compileSearchMatcher(query, useRegex, caseSensitive)
	if err != nil {
		return "", err
	}
	var results []string
	walkErr := filepath.WalkDir(state.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == state.Root {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := state.relativePath(path)
		if err != nil || !searchGlobMatches(glob, relative) {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(file, maxAgentReadBytes))
		_ = file.Close()
		if err != nil || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			return nil
		}
		for index, line := range strings.Split(string(data), "\n") {
			column := matcher(line)
			if column < 0 {
				continue
			}
			results = append(results, fmt.Sprintf("%s:%d:%d  %s", relative, index+1, column+1, line))
			if len(results) >= maxAgentSearchMatches {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(results) == 0 {
		return "No matches found.", nil
	}
	return strings.Join(results, "\n"), nil
}

func compactAgentToolOutput(value string) string {
	if executable, err := exec.LookPath(executableName("rtk")); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "pipe")
		command.Stdin = strings.NewReader(value)
		var output bytes.Buffer
		command.Stdout = &limitedWriter{writer: &output, remaining: maxAgentToolOutputBytes}
		if err := command.Run(); err == nil && strings.TrimSpace(output.String()) != "" {
			return boundString(output.String(), maxAgentToolOutputBytes)
		}
	}

	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		result = append(result, line)
	}
	return boundString(strings.Join(result, "\n"), maxAgentToolOutputBytes)
}

func extractRouterText(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				if text, ok := object["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

func (m *agentManager) getRun(id string) (AgentRun, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.runs[id]
	if state == nil {
		return AgentRun{}, false
	}
	copyValue := state.value
	copyValue.Usage = cloneAnyMap(state.value.Usage)
	return copyValue, true
}

func (m *agentManager) updateRun(id string, update func(*AgentRun)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.runs[id]
	if state == nil {
		return
	}
	update(&state.value)
	state.value.UpdatedAt = time.Now().UTC()
}

func (m *agentManager) finishRun(id, status, activity, output string, runErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.runs[id]
	if state == nil || state.value.CompletedAt != nil {
		return
	}
	now := time.Now().UTC()
	state.value.Status = status
	state.value.Phase = status
	state.value.Activity = activity
	state.value.Output = boundString(output, maxAgentOutputPreview)
	if runErr != nil {
		state.value.Error = runErr.Error()
	}
	state.value.UpdatedAt = now
	state.value.CompletedAt = &now
	state.cancel()
	for index := range m.slots {
		if m.slots[index].ID == state.value.SlotID && m.slots[index].ActiveRunID == id {
			m.slots[index].Busy = false
			m.slots[index].ActiveRunID = ""
			break
		}
	}
}

func (m *agentManager) cancelRun(id string) error {
	m.mu.Lock()
	state := m.runs[id]
	if state == nil {
		m.mu.Unlock()
		return fmt.Errorf("unknown agent run %q", id)
	}
	if state.value.Status != "queued" && state.value.Status != "running" {
		m.mu.Unlock()
		return fmt.Errorf("agent run %s is already %s", id, state.value.Status)
	}
	state.cancel()
	m.mu.Unlock()
	m.finishRun(id, "cancelled", "Cancelled by ChatGPT or the local operator", "", context.Canceled)
	return nil
}

func (m *agentManager) refreshRouterStatus(ctx context.Context) RouterStatus {
	status := m.router.status(ctx)
	m.mu.Lock()
	m.routerStatus = status
	m.mu.Unlock()
	return status
}

func (m *agentManager) routerStatusSnapshot() RouterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := m.routerStatus
	result.Models = append([]RouterModel(nil), m.routerStatus.Models...)
	return result
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprint(value)
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func boolValue(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func boundString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
}

func sortedAgentSlots(slots []AgentSlot) []AgentSlot {
	result := append([]AgentSlot(nil), slots...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
