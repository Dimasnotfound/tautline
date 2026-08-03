package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultSkillSearchLimit = 5
	maxSkillSearchLimit     = 12
	defaultSkillReadBytes   = 128 * 1024
	maxSkillReadBytes       = 512 * 1024
	maxBridgeOutputBytes    = 2 * 1024 * 1024
)

//go:embed hermes_skill_bridge.py
var embeddedHermesSkillBridge []byte

type skillBridgeConfig struct {
	Home           string
	AgentDir       string
	Python         string
	Snapshot       string
	AvailableTools []string
	Toolsets       []string
}

type skillSnapshotInfo struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	Fingerprint string `json:"fingerprint"`
	SHA256      string `json:"sha256,omitempty"`
	Bytes       int64  `json:"bytes"`
	MTimeNS     int64  `json:"mtime_ns"`
}

type skillIndexStats struct {
	Installed  int `json:"installed"`
	Discovered int `json:"discovered"`
	Active     int `json:"active"`
	Compatible int `json:"compatible"`
	Categories int `json:"categories"`
}

type skillConditions struct {
	FallbackToolsets []string `json:"fallback_for_toolsets,omitempty"`
	RequiresToolsets []string `json:"requires_toolsets,omitempty"`
	FallbackTools    []string `json:"fallback_for_tools,omitempty"`
	RequiresTools    []string `json:"requires_tools,omitempty"`
}

type skillMetadata struct {
	Name                  string          `json:"name"`
	Identifier            string          `json:"identifier"`
	Category              string          `json:"category"`
	Description           string          `json:"description,omitempty"`
	Tags                  []string        `json:"tags,omitempty"`
	RelatedSkills         []string        `json:"related_skills,omitempty"`
	Platforms             []string        `json:"platforms,omitempty"`
	Conditions            skillConditions `json:"conditions,omitempty"`
	Active                bool            `json:"active"`
	PlatformCompatible    bool            `json:"platform_compatible"`
	EnvironmentCompatible bool            `json:"environment_compatible"`
	ToolCompatible        bool            `json:"tool_compatible"`
	Compatible            bool            `json:"compatible"`
	CompatibilityReasons  []string        `json:"compatibility_reasons,omitempty"`
	SkillDir              string          `json:"skill_dir,omitempty"`
}

type skillIndexResponse struct {
	Success  bool              `json:"success"`
	Error    string            `json:"error,omitempty"`
	Skills   []skillMetadata   `json:"skills,omitempty"`
	Stats    skillIndexStats   `json:"stats"`
	Snapshot skillSnapshotInfo `json:"snapshot"`
	Source   string            `json:"source,omitempty"`
}

type skillSearchMatch struct {
	Name                 string   `json:"name"`
	Identifier           string   `json:"identifier"`
	Category             string   `json:"category"`
	Description          string   `json:"description,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	RelatedSkills        []string `json:"relatedSkills,omitempty"`
	Score                int      `json:"score"`
	MatchedOn            []string `json:"matchedOn,omitempty"`
	Compatible           bool     `json:"compatible"`
	CompatibilityReasons []string `json:"compatibilityReasons,omitempty"`
}

type skillSearchView struct {
	Kind      string             `json:"kind"`
	Title     string             `json:"title"`
	Summary   string             `json:"summary"`
	Query     string             `json:"query"`
	Results   []skillSearchMatch `json:"results"`
	Stats     skillIndexStats    `json:"stats"`
	Snapshot  skillSnapshotInfo  `json:"snapshot"`
	CacheHit  bool               `json:"cacheHit"`
	Source    string             `json:"source"`
	Truncated bool               `json:"truncated,omitempty"`
}

type skillConfigStatus struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Configured  bool   `json:"configured"`
	Sensitive   bool   `json:"sensitive"`
	Value       string `json:"value"`
}

type skillEnvironmentStatus struct {
	Name       string `json:"name"`
	Optional   bool   `json:"optional"`
	Configured bool   `json:"configured"`
}

type skillBridgeViewResponse struct {
	Success                      bool                     `json:"success"`
	Error                        string                   `json:"error,omitempty"`
	Name                         string                   `json:"name"`
	Identifier                   string                   `json:"identifier"`
	Category                     string                   `json:"category"`
	Description                  string                   `json:"description,omitempty"`
	Tags                         []string                 `json:"tags,omitempty"`
	RelatedSkills                []string                 `json:"related_skills,omitempty"`
	Content                      string                   `json:"content"`
	LinkedFiles                  map[string][]string      `json:"linked_files,omitempty"`
	ReadinessStatus              string                   `json:"readiness_status"`
	SetupNeeded                  bool                     `json:"setup_needed"`
	SetupNote                    string                   `json:"setup_note,omitempty"`
	RequiredEnvironmentVariables []skillEnvironmentStatus `json:"required_environment_variables,omitempty"`
	MissingCredentialFiles       []string                 `json:"missing_credential_files,omitempty"`
	Config                       []skillConfigStatus      `json:"config,omitempty"`
	Bytes                        int64                    `json:"bytes"`
	Truncated                    bool                     `json:"truncated,omitempty"`
	Redacted                     bool                     `json:"redacted,omitempty"`
	Compatibility                skillMetadata            `json:"compatibility"`
	SkillDir                     string                   `json:"skill_dir,omitempty"`
	Source                       string                   `json:"source"`
}

type skillView struct {
	Kind                         string                   `json:"kind"`
	Title                        string                   `json:"title"`
	Summary                      string                   `json:"summary"`
	Name                         string                   `json:"name"`
	Identifier                   string                   `json:"identifier"`
	Category                     string                   `json:"category"`
	Description                  string                   `json:"description,omitempty"`
	Tags                         []string                 `json:"tags,omitempty"`
	RelatedSkills                []string                 `json:"relatedSkills,omitempty"`
	Content                      string                   `json:"content"`
	LinkedFiles                  map[string][]string      `json:"linkedFiles,omitempty"`
	ReadinessStatus              string                   `json:"readinessStatus"`
	SetupNeeded                  bool                     `json:"setupNeeded"`
	SetupNote                    string                   `json:"setupNote,omitempty"`
	RequiredEnvironmentVariables []skillEnvironmentStatus `json:"requiredEnvironmentVariables,omitempty"`
	MissingCredentialFiles       []string                 `json:"missingCredentialFiles,omitempty"`
	Config                       []skillConfigStatus      `json:"config,omitempty"`
	Compatibility                skillMetadata            `json:"compatibility"`
	SkillDir                     string                   `json:"skillDir,omitempty"`
	Source                       string                   `json:"source"`
	Stats                        toolStats                `json:"stats"`
	Truncated                    bool                     `json:"truncated,omitempty"`
	Redacted                     bool                     `json:"redacted,omitempty"`
}

type skillBridgeFileResponse struct {
	Success       bool          `json:"success"`
	Error         string        `json:"error,omitempty"`
	Name          string        `json:"name"`
	Identifier    string        `json:"identifier"`
	Category      string        `json:"category"`
	File          string        `json:"file"`
	Content       string        `json:"content"`
	FileType      string        `json:"file_type,omitempty"`
	IsBinary      bool          `json:"is_binary,omitempty"`
	Bytes         int64         `json:"bytes"`
	Truncated     bool          `json:"truncated,omitempty"`
	Redacted      bool          `json:"redacted,omitempty"`
	Compatibility skillMetadata `json:"compatibility"`
	Source        string        `json:"source"`
}

type skillFileView struct {
	Kind          string        `json:"kind"`
	Title         string        `json:"title"`
	Summary       string        `json:"summary"`
	Name          string        `json:"name"`
	Identifier    string        `json:"identifier"`
	Category      string        `json:"category"`
	File          string        `json:"file"`
	Language      string        `json:"language,omitempty"`
	Content       string        `json:"content"`
	IsBinary      bool          `json:"isBinary,omitempty"`
	Compatibility skillMetadata `json:"compatibility"`
	Source        string        `json:"source"`
	Stats         toolStats     `json:"stats"`
	Truncated     bool          `json:"truncated,omitempty"`
	Redacted      bool          `json:"redacted,omitempty"`
}

type bridgeRequest struct {
	Action            string   `json:"action"`
	Name              string   `json:"name,omitempty"`
	FilePath          string   `json:"file_path,omitempty"`
	MaxBytes          int      `json:"max_bytes,omitempty"`
	AllowIncompatible bool     `json:"allow_incompatible,omitempty"`
	AvailableTools    []string `json:"available_tools,omitempty"`
	AvailableToolsets []string `json:"available_toolsets,omitempty"`
	FullView          bool     `json:"full_view,omitempty"`
}

type skillCacheState struct {
	mu       sync.Mutex
	key      string
	loadedAt time.Time
	response skillIndexResponse
}

var hermesSkillCache skillCacheState

var (
	sensitiveAssignmentRE = regexp.MustCompile(`(?i)(\b(?:token|secret|password|passwd|api[_-]?key|access[_-]?key|private[_-]?key|cookie|credential|authorization|client[_-]?secret)\b\s*[:=]\s*)(Bearer\s+[^\s,;]+|[^\s,;]+|"[^"]*"|'[^']*')`)
	bearerSecretRE        = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]{12,}`)
	urlSecretRE           = regexp.MustCompile(`(?i)([?&](?:token|secret|password|api[_-]?key|access[_-]?key)\s*=)[^&#\s]+`)
)

func registerSkillTools(s *server.MCPServer) {
	searchTool := mcp.NewTool("skills_search",
		mcp.WithTitleAnnotation("Search Hermes skills"),
		mcp.WithDescription("Search the installed Hermes Agent skills using its read-only loader. For every non-trivial task, call this first with the user's resolved request, then load relevant compatible results with skill_view before using workspace tools."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The user's complete task or a precise description of the expertise needed")),
		mcp.WithNumber("limit", mcp.Description("Maximum results, default 5 and maximum 12")),
		mcp.WithBoolean("include_incompatible", mcp.Description("Include skills excluded by platform, environment, or available-tool filters")),
		mcp.WithOutputSchema[skillSearchView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(searchTool, handleSkillsSearch)

	viewTool := mcp.NewTool("skill_view",
		mcp.WithTitleAnnotation("Load Hermes skill"),
		mcp.WithDescription("Load one compatible Hermes SKILL.md through the read-only bridge. Follow the loaded workflow when it is relevant, while keeping system and user instructions higher priority. Secret configuration values are never returned."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Skill name or categorized identifier returned by skills_search")),
		mcp.WithNumber("max_bytes", mcp.Description("Maximum UTF-8 content bytes, default 131072 and maximum 524288")),
		mcp.WithBoolean("allow_incompatible", mcp.Description("Explicitly load a tool-incompatible skill; platform-incompatible skills may still be rejected by Hermes")),
		mcp.WithOutputSchema[skillView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(viewTool, handleSkillView)

	readFileTool := mcp.NewTool("skill_read_file",
		mcp.WithTitleAnnotation("Read Hermes skill file"),
		mcp.WithDescription("Read one supporting file from references, templates, scripts, or assets inside an installed Hermes skill. The Hermes path guard is used and secret-looking values are redacted."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Skill name or categorized identifier")),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("Relative supporting-file path returned by skill_view")),
		mcp.WithNumber("max_bytes", mcp.Description("Maximum UTF-8 content bytes, default 131072 and maximum 524288")),
		mcp.WithBoolean("allow_incompatible", mcp.Description("Explicitly read a file from a tool-incompatible skill")),
		mcp.WithOutputSchema[skillFileView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(readFileTool, handleSkillReadFile)
}

func defaultSkillBridgeConfig() skillBridgeConfig {
	home := strings.TrimSpace(os.Getenv("DEVSPACE_HERMES_HOME"))
	if home == "" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			home = filepath.Join(local, "hermes")
		} else if userHome, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(userHome, ".hermes")
		}
	}
	agentDir := strings.TrimSpace(os.Getenv("DEVSPACE_HERMES_AGENT_DIR"))
	if agentDir == "" {
		agentDir = filepath.Join(home, "hermes-agent")
	}
	pythonPath := strings.TrimSpace(os.Getenv("DEVSPACE_HERMES_PYTHON"))
	if pythonPath == "" {
		if runtime.GOOS == "windows" {
			pythonPath = filepath.Join(agentDir, "venv", "Scripts", "python.exe")
		} else {
			pythonPath = filepath.Join(agentDir, "venv", "bin", "python")
		}
	}
	snapshot := strings.TrimSpace(os.Getenv("DEVSPACE_HERMES_SKILLS_SNAPSHOT"))
	if snapshot == "" {
		snapshot = filepath.Join(home, ".skills_prompt_snapshot.json")
	}
	availableTools := splitCSVEnv("DEVSPACE_SKILL_AVAILABLE_TOOLS", []string{
		"open_workspace", "search", "read", "write", "edit", "bash", "artifact_read", "show_changes",
		"skills_search", "skill_view", "skill_read_file",
	})
	toolsets := splitCSVEnv("DEVSPACE_SKILL_AVAILABLE_TOOLSETS", []string{"terminal", "files"})
	return skillBridgeConfig{
		Home:           filepath.Clean(home),
		AgentDir:       filepath.Clean(agentDir),
		Python:         filepath.Clean(pythonPath),
		Snapshot:       filepath.Clean(snapshot),
		AvailableTools: availableTools,
		Toolsets:       toolsets,
	}
}

func splitCSVEnv(name string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return append([]string(nil), fallback...)
	}
	seen := map[string]bool{}
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func bridgeScriptPath() (string, error) {
	digest := sha256.Sum256(embeddedHermesSkillBridge)
	name := "tautline-hermes-skill-bridge-" + hex.EncodeToString(digest[:8]) + ".py"
	directory := filepath.Join(os.TempDir(), "tautline")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, name)
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, embeddedHermesSkillBridge) {
		return path, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(path, embeddedHermesSkillBridge, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func runSkillBridge(ctx context.Context, request bridgeRequest, output any) error {
	config := defaultSkillBridgeConfig()
	if info, err := os.Stat(config.Python); err != nil || info.IsDir() {
		if err == nil {
			err = errors.New("path is a directory")
		}
		return fmt.Errorf("Hermes Python is unavailable at %s: %w", config.Python, err)
	}
	if info, err := os.Stat(config.AgentDir); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("path is not a directory")
		}
		return fmt.Errorf("Hermes Agent directory is unavailable at %s: %w", config.AgentDir, err)
	}
	script, err := bridgeScriptPath()
	if err != nil {
		return fmt.Errorf("prepare Hermes skill bridge: %w", err)
	}
	request.AvailableTools = append([]string(nil), config.AvailableTools...)
	request.AvailableToolsets = append([]string(nil), config.Toolsets...)
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, config.Python, script)
	cmd.Dir = config.AgentDir
	cmd.Env = append(os.Environ(),
		"DEVSPACE_HERMES_HOME="+config.Home,
		"DEVSPACE_HERMES_AGENT_DIR="+config.AgentDir,
		"DEVSPACE_HERMES_SKILLS_SNAPSHOT="+config.Snapshot,
		"HERMES_HOME="+config.Home,
		"PYTHONUTF8=1",
		"PYTHONIOENCODING=utf-8",
		"DEVSPACE_HERMES_READ_ONLY=1",
	)
	cmd.Stdin = bytes.NewReader(payload)
	stdout := &boundedBuffer{limit: maxBridgeOutputBytes}
	stderr := &boundedBuffer{limit: 64 * 1024}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Hermes skill bridge timed out or was canceled: %w", ctx.Err())
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Hermes skill bridge failed: %s", message)
	}
	if stdout.Truncated() {
		return errors.New("Hermes skill bridge response exceeded 2 MiB")
	}
	if err := json.Unmarshal([]byte(stdout.String()), output); err != nil {
		return fmt.Errorf("invalid Hermes skill bridge response: %w", err)
	}
	return nil
}

func skillCacheKey(config skillBridgeConfig) string {
	parts := []string{
		config.Home,
		config.AgentDir,
		config.Python,
		strings.Join(config.AvailableTools, ","),
		strings.Join(config.Toolsets, ","),
	}
	if info, err := os.Stat(config.Snapshot); err == nil {
		parts = append(parts, config.Snapshot, strconv.FormatInt(info.ModTime().UnixNano(), 10), strconv.FormatInt(info.Size(), 10))
	} else {
		parts = append(parts, config.Snapshot, "missing")
	}
	return strings.Join(parts, "|")
}

func loadSkillIndex(ctx context.Context) (skillIndexResponse, bool, error) {
	config := defaultSkillBridgeConfig()
	key := skillCacheKey(config)
	hermesSkillCache.mu.Lock()
	if hermesSkillCache.key == key && hermesSkillCache.response.Success {
		cached := hermesSkillCache.response
		hermesSkillCache.mu.Unlock()
		return cached, true, nil
	}
	hermesSkillCache.mu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var response skillIndexResponse
	if err := runSkillBridge(runCtx, bridgeRequest{Action: "list"}, &response); err != nil {
		return skillIndexResponse{}, false, err
	}
	if !response.Success {
		return skillIndexResponse{}, false, errors.New(response.Error)
	}

	hermesSkillCache.mu.Lock()
	hermesSkillCache.key = key
	hermesSkillCache.loadedAt = time.Now()
	hermesSkillCache.response = response
	hermesSkillCache.mu.Unlock()
	return response, false, nil
}

func clearSkillIndexCache() {
	hermesSkillCache.mu.Lock()
	hermesSkillCache.key = ""
	hermesSkillCache.loadedAt = time.Time{}
	hermesSkillCache.response = skillIndexResponse{}
	hermesSkillCache.mu.Unlock()
}

func handleSkillsSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := strings.TrimSpace(argStr(req, "query"))
	if query == "" {
		return mcp.NewToolResultError("query must not be empty"), nil
	}
	limit := clampInt(argInt(req, "limit", defaultSkillSearchLimit), 1, maxSkillSearchLimit)
	includeIncompatible := argBool(req, "include_incompatible", false)
	index, cacheHit, err := loadSkillIndex(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	matches := rankSkills(query, index.Skills, includeIncompatible)
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	view := skillSearchView{
		Kind:      "skills_search",
		Title:     fmt.Sprintf("Found %d skills", len(matches)),
		Summary:   fmt.Sprintf("%d matches · %d compatible of %d installed", len(matches), index.Stats.Compatible, index.Stats.Installed),
		Query:     query,
		Results:   matches,
		Stats:     index.Stats,
		Snapshot:  index.Snapshot,
		CacheHit:  cacheHit,
		Source:    index.Source,
		Truncated: truncated,
	}
	fallback := fmt.Sprintf("Matched %d Hermes skills for %q.", len(matches), query)
	if len(matches) == 0 {
		fallback = fmt.Sprintf("No compatible Hermes skill matched %q.", query)
	}
	return newToolResult("skills_search", view, view, fallback), nil
}

func handleSkillView(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := strings.TrimSpace(argStr(req, "name"))
	if name == "" {
		return mcp.NewToolResultError("name must not be empty"), nil
	}
	limit := clampInt(argInt(req, "max_bytes", defaultSkillReadBytes), 1024, maxSkillReadBytes)
	request := bridgeRequest{
		Action:            "view",
		Name:              name,
		MaxBytes:          limit,
		AllowIncompatible: argBool(req, "allow_incompatible", false),
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var response skillBridgeViewResponse
	if err := runSkillBridge(runCtx, request, &response); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "Hermes skill loader rejected the requested skill"
		}
		return mcp.NewToolResultError(response.Error), nil
	}
	content, redacted := redactSensitiveText(response.Content)
	response.Content = content
	response.Redacted = response.Redacted || redacted
	lines := countLines(response.Content)
	view := skillView{
		Kind:                         "skill",
		Title:                        "Loaded skill",
		Summary:                      skillLoadSummary(response),
		Name:                         response.Name,
		Identifier:                   response.Identifier,
		Category:                     response.Category,
		Description:                  response.Description,
		Tags:                         response.Tags,
		RelatedSkills:                response.RelatedSkills,
		Content:                      response.Content,
		LinkedFiles:                  response.LinkedFiles,
		ReadinessStatus:              response.ReadinessStatus,
		SetupNeeded:                  response.SetupNeeded,
		SetupNote:                    response.SetupNote,
		RequiredEnvironmentVariables: response.RequiredEnvironmentVariables,
		MissingCredentialFiles:       response.MissingCredentialFiles,
		Config:                       response.Config,
		Compatibility:                response.Compatibility,
		SkillDir:                     response.SkillDir,
		Source:                       response.Source,
		Stats:                        toolStats{Bytes: response.Bytes, Lines: lines, Files: countLinkedSkillFiles(response.LinkedFiles)},
		Truncated:                    response.Truncated,
		Redacted:                     response.Redacted,
	}
	fallback := fmt.Sprintf("Loaded Hermes skill %s · %s.", response.Name, response.ReadinessStatus)
	if response.Redacted {
		fallback += " Sensitive values were redacted."
	}
	return newToolResult("skill_view", compactSkillView(view), view, fallback), nil
}

func handleSkillReadFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := strings.TrimSpace(argStr(req, "name"))
	filePath := strings.TrimSpace(argStr(req, "file_path"))
	if name == "" || filePath == "" {
		return mcp.NewToolResultError("name and file_path are required"), nil
	}
	limit := clampInt(argInt(req, "max_bytes", defaultSkillReadBytes), 1024, maxSkillReadBytes)
	request := bridgeRequest{
		Action:            "read_file",
		Name:              name,
		FilePath:          filePath,
		MaxBytes:          limit,
		AllowIncompatible: argBool(req, "allow_incompatible", false),
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var response skillBridgeFileResponse
	if err := runSkillBridge(runCtx, request, &response); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "Hermes skill loader rejected the supporting file"
		}
		return mcp.NewToolResultError(response.Error), nil
	}
	content, redacted := redactSensitiveText(response.Content)
	response.Content = content
	response.Redacted = response.Redacted || redacted
	lines := countLines(response.Content)
	view := skillFileView{
		Kind:          "skill_file",
		Title:         "Read skill file",
		Summary:       fmt.Sprintf("%d lines · %d bytes", lines, response.Bytes),
		Name:          response.Name,
		Identifier:    response.Identifier,
		Category:      response.Category,
		File:          filepath.ToSlash(response.File),
		Language:      languageFromPath(response.File),
		Content:       withLineNumbers(response.Content),
		IsBinary:      response.IsBinary,
		Compatibility: response.Compatibility,
		Source:        response.Source,
		Stats:         toolStats{Bytes: response.Bytes, Lines: lines, Files: 1},
		Truncated:     response.Truncated,
		Redacted:      response.Redacted,
	}
	fallback := fmt.Sprintf("Read %s from Hermes skill %s · %d lines.", view.File, view.Name, lines)
	if response.Redacted {
		fallback += " Sensitive values were redacted."
	}
	return newToolResult("skill_read_file", view, view, fallback), nil
}

func compactSkillView(view skillView) skillView {
	compact := view
	var modelTruncated bool
	compact.Content, modelTruncated = truncateUTF8(view.Content, 48*1024)
	compact.Truncated = view.Truncated || modelTruncated
	compact.SkillDir = ""
	return compact
}

func skillLoadSummary(response skillBridgeViewResponse) string {
	parts := []string{response.Category, response.ReadinessStatus}
	if count := countLinkedSkillFiles(response.LinkedFiles); count > 0 {
		parts = append(parts, fmt.Sprintf("%d supporting files", count))
	}
	if response.Redacted {
		parts = append(parts, "secrets redacted")
	}
	return strings.Join(parts, " · ")
}

func countLinkedSkillFiles(files map[string][]string) int {
	count := 0
	for _, values := range files {
		count += len(values)
	}
	return count
}

func argBool(req mcp.CallToolRequest, key string, fallback bool) bool {
	args := req.GetArguments()
	switch value := args[key].(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func redactSensitiveText(value string) (string, bool) {
	original := value
	value = sensitiveAssignmentRE.ReplaceAllString(value, `${1}[REDACTED]`)
	value = bearerSecretRE.ReplaceAllString(value, `${1}[REDACTED]`)
	value = urlSecretRE.ReplaceAllString(value, `${1}[REDACTED]`)
	return value, value != original
}

func rankSkills(query string, skills []skillMetadata, includeIncompatible bool) []skillSearchMatch {
	queryNormalized := normalizeSkillSearchText(query)
	queryTokens := meaningfulSkillTokens(queryNormalized)
	var matches []skillSearchMatch
	for _, skill := range skills {
		if !includeIncompatible && !skill.Compatible {
			continue
		}
		score, matchedOn := scoreSkill(queryNormalized, queryTokens, skill)
		if score <= 0 {
			continue
		}
		matches = append(matches, skillSearchMatch{
			Name:                 skill.Name,
			Identifier:           skill.Identifier,
			Category:             skill.Category,
			Description:          skill.Description,
			Tags:                 append([]string(nil), skill.Tags...),
			RelatedSkills:        append([]string(nil), skill.RelatedSkills...),
			Score:                score,
			MatchedOn:            matchedOn,
			Compatible:           skill.Compatible,
			CompatibilityReasons: append([]string(nil), skill.CompatibilityReasons...),
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].Compatible != matches[j].Compatible {
			return matches[i].Compatible
		}
		if matches[i].Category != matches[j].Category {
			return matches[i].Category < matches[j].Category
		}
		return matches[i].Name < matches[j].Name
	})
	return matches
}

func scoreSkill(query string, queryTokens []string, skill skillMetadata) (int, []string) {
	name := normalizeSkillSearchText(skill.Name)
	identifier := normalizeSkillSearchText(skill.Identifier)
	category := normalizeSkillSearchText(skill.Category)
	description := normalizeSkillSearchText(skill.Description)
	tags := normalizeSkillSearchText(strings.Join(skill.Tags, " "))
	related := normalizeSkillSearchText(strings.Join(skill.RelatedSkills, " "))
	nameTokens := tokenSet(name)
	identifierTokens := tokenSet(identifier)
	categoryTokens := tokenSet(category)
	descriptionTokens := tokenSet(description)
	tagTokens := tokenSet(tags)
	relatedTokens := tokenSet(related)

	score := 0
	matched := map[string]bool{}
	if query == name || query == identifier {
		score += 180
		matched["exact name"] = true
	} else {
		if strings.Contains(name, query) && len(query) >= 3 {
			score += 95
			matched["name"] = true
		}
		if strings.Contains(identifier, query) && len(query) >= 3 {
			score += 70
			matched["identifier"] = true
		}
	}
	allTokensMatched := len(queryTokens) > 0
	for _, token := range queryTokens {
		tokenMatched := false
		switch {
		case nameTokens[token]:
			score += 32
			matched["name"] = true
			tokenMatched = true
		case identifierTokens[token]:
			score += 25
			matched["identifier"] = true
			tokenMatched = true
		case hasTokenPrefix(nameTokens, token):
			score += 22
			matched["name prefix"] = true
			tokenMatched = true
		case tagTokens[token]:
			score += 20
			matched["tags"] = true
			tokenMatched = true
		case categoryTokens[token]:
			score += 15
			matched["category"] = true
			tokenMatched = true
		case descriptionTokens[token]:
			score += 10
			matched["description"] = true
			tokenMatched = true
		case relatedTokens[token]:
			score += 7
			matched["related skills"] = true
			tokenMatched = true
		case strings.Contains(description, token) && len(token) >= 4:
			score += 5
			matched["description"] = true
			tokenMatched = true
		}
		if !tokenMatched {
			allTokensMatched = false
		}
	}
	if allTokensMatched && len(queryTokens) > 1 {
		score += 24
		matched["all query terms"] = true
	}
	if !skill.Compatible {
		score -= 35
	}
	matchedOn := make([]string, 0, len(matched))
	for value := range matched {
		matchedOn = append(matchedOn, value)
	}
	sort.Strings(matchedOn)
	return score, matchedOn
}

func hasTokenPrefix(tokens map[string]bool, query string) bool {
	if len(query) < 3 {
		return false
	}
	for token := range tokens {
		if strings.HasPrefix(token, query) || strings.HasPrefix(query, token) {
			return true
		}
	}
	return false
}

func normalizeSkillSearchText(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	builder.Grow(len(value))
	space := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			space = false
		} else if !space {
			builder.WriteByte(' ')
			space = true
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func meaningfulSkillTokens(value string) []string {
	stopWords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
		"be": true, "by": true, "for": true, "from": true, "how": true, "i": true,
		"in": true, "is": true, "it": true, "of": true, "on": true, "or": true,
		"that": true, "the": true, "this": true, "to": true, "use": true, "with": true,
		"yang": true, "dan": true, "atau": true, "dari": true, "untuk": true, "dengan": true,
		"ini": true, "itu": true, "saya": true, "anda": true, "agar": true, "pada": true,
		"ke": true, "di": true, "dalam": true, "sebuah": true, "buat": true, "membuat": true,
		"tolong": true, "bagaimana": true, "bisa": true, "dapat": true, "lakukan": true,
	}
	seen := map[string]bool{}
	var result []string
	for _, token := range strings.Fields(value) {
		if len([]rune(token)) < 2 || stopWords[token] || seen[token] {
			continue
		}
		seen[token] = true
		result = append(result, token)
	}
	return result
}

func tokenSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, token := range strings.Fields(value) {
		result[token] = true
	}
	return result
}
