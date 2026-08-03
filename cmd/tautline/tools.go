package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pmezard/go-difflib/difflib"
)

const (
	defaultReadBytes      = 64 * 1024
	maxReadBytes          = 256 * 1024
	maxCommandBytes       = 128 * 1024
	defaultCommandSecs    = 120
	maxCommandSecs        = 300
	maxWorkspaceEntries   = 350
	maxWorkspaceDepth     = 5
	modelWorkspaceEntries = 20
)

type viewFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

type toolStats struct {
	Entries     int   `json:"entries,omitempty"`
	Directories int   `json:"directories,omitempty"`
	Files       int   `json:"files,omitempty"`
	Bytes       int64 `json:"bytes,omitempty"`
	Lines       int   `json:"lines,omitempty"`
	Added       int   `json:"added,omitempty"`
	Removed     int   `json:"removed,omitempty"`
	Occurrences int   `json:"occurrences,omitempty"`
	DurationMS  int64 `json:"durationMs,omitempty"`
	ExitCode    *int  `json:"exitCode,omitempty"`
}

type workspaceView struct {
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary,omitempty"`
	WorkspaceID string     `json:"workspaceId"`
	Path        string     `json:"path"`
	Files       []viewFile `json:"files"`
	Stats       toolStats  `json:"stats"`
	Truncated   bool       `json:"truncated,omitempty"`
}

type workspaceModelView struct {
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary,omitempty"`
	WorkspaceID string     `json:"workspaceId"`
	Path        string     `json:"path"`
	Files       []viewFile `json:"files"`
	Stats       toolStats  `json:"stats"`
	Truncated   bool       `json:"truncated,omitempty"`
}

type fileView struct {
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary,omitempty"`
	WorkspaceID    string    `json:"workspaceId"`
	Path           string    `json:"path"`
	Language       string    `json:"language,omitempty"`
	Content        string    `json:"content"`
	Stats          toolStats `json:"stats"`
	Truncated      bool      `json:"truncated,omitempty"`
	SHA256         string    `json:"sha256,omitempty"`
	StartLine      int       `json:"startLine,omitempty"`
	EndLine        int       `json:"endLine,omitempty"`
	TotalLines     int       `json:"totalLines,omitempty"`
	NextCursor     string    `json:"nextCursor,omitempty"`
	PreviousCursor string    `json:"previousCursor,omitempty"`
	ModifiedUnix   int64     `json:"modifiedUnix,omitempty"`
}

type diffView struct {
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary,omitempty"`
	WorkspaceID string    `json:"workspaceId"`
	Path        string    `json:"path"`
	Operation   string    `json:"operation"`
	Diff        string    `json:"diff"`
	Stats       toolStats `json:"stats"`
	Truncated   bool      `json:"truncated,omitempty"`
}

type commandView struct {
	Kind               string             `json:"kind"`
	Title              string             `json:"title"`
	Summary            string             `json:"summary,omitempty"`
	WorkspaceID        string             `json:"workspaceId"`
	Path               string             `json:"path"`
	Command            string             `json:"command"`
	Output             string             `json:"output"`
	Success            bool               `json:"success"`
	Stats              toolStats          `json:"stats"`
	Truncated          bool               `json:"truncated,omitempty"`
	Artifact           *artifactReference `json:"artifact,omitempty"`
	IncludedRanges     []lineRange        `json:"includedRanges,omitempty"`
	OmittedLines       int                `json:"omittedLines,omitempty"`
	Redacted           bool               `json:"redacted,omitempty"`
	StorageUnavailable bool               `json:"storageUnavailable,omitempty"`
}

func registerTools(s *server.MCPServer) {
	openWorkspaceTool := mcp.NewTool("open_workspace",
		mcp.WithTitleAnnotation("Open workspace"),
		mcp.WithDescription("Open one project folder and return a reusable workspace_id. Call this once per project, then use relative paths with every other Tautline workspace tool."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute project directory inside an allowed root")),
		mcp.WithOutputSchema[workspaceModelView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	if activeWidgetMode != widgetModeOff {
		setWidgetMeta(&openWorkspaceTool, activityWidgetURI, "Opening workspace", "Workspace ready")
	}
	s.AddTool(openWorkspaceTool, handleOpenWorkspace)

	activityTool := mcp.NewTool("activity_snapshot",
		mcp.WithTitleAnnotation("Read Tautline activity"),
		mcp.WithDescription("Read the current activity timeline for one open workspace. This app-only tool feeds the single Tautline activity widget."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("event_id", mcp.Description("Optional event to inspect in detail")),
		mcp.WithOutputSchema[activitySnapshot](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	setAppOnlyToolMeta(&activityTool)
	s.AddTool(activityTool, handleActivitySnapshot)

	searchTool := mcp.NewTool("search",
		mcp.WithTitleAnnotation("Search workspace"),
		mcp.WithDescription("Search UTF-8 files in an open workspace before reading large files. Returns bounded exact matches with line numbers, context, and file hashes; it never modifies files."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Literal text or regular expression to find")),
		mcp.WithBoolean("regex", mcp.Description("Interpret query as a Go regular expression")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Use case-sensitive matching; default false")),
		mcp.WithString("glob", mcp.Description("Optional comma-separated file globs such as *.go,internal/*.go")),
		mcp.WithNumber("context_lines", mcp.Description("Exact surrounding lines per match, default 2 and maximum 12")),
		mcp.WithNumber("max_results", mcp.Description("Maximum matches, default 20 and maximum 100")),
		mcp.WithBoolean("include_hidden", mcp.Description("Include hidden files and directories not otherwise excluded")),
		mcp.WithOutputSchema[searchView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(searchTool, handleWorkspaceSearch)

	readTool := mcp.NewTool("read",
		mcp.WithTitleAnnotation("Read file"),
		mcp.WithDescription("Read one UTF-8 file from an open workspace. Legacy calls still read from the start. For large files, use start_line/end_line, head, tail, max_lines, or the returned cursor; pass expected_sha256 to reject stale reads."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to the workspace root")),
		mcp.WithNumber("max_bytes", mcp.Description("Legacy response limit in bytes, default 65536 and maximum 262144")),
		mcp.WithNumber("start_line", mcp.Description("Optional first line for a context-safe window")),
		mcp.WithNumber("end_line", mcp.Description("Optional last line for a context-safe window")),
		mcp.WithNumber("head", mcp.Description("Read the first N lines, maximum 2000")),
		mcp.WithNumber("tail", mcp.Description("Read the last N lines, maximum 2000")),
		mcp.WithNumber("max_lines", mcp.Description("Window size when end_line is omitted, default 200 and maximum 2000")),
		mcp.WithString("cursor", mcp.Description("Continuation cursor returned by an earlier read")),
		mcp.WithString("expected_sha256", mcp.Description("Reject the read if the current file hash differs")),
		mcp.WithOutputSchema[fileView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(readTool, handleRead)

	writeTool := mcp.NewTool("write",
		mcp.WithTitleAnnotation("Write file"),
		mcp.WithDescription("Atomically create or replace one UTF-8 file in an open workspace. Use edit for a small unique replacement."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to the workspace root")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Complete replacement content")),
		mcp.WithOutputSchema[diffView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(writeTool, handleWrite)

	editTool := mcp.NewTool("edit",
		mcp.WithTitleAnnotation("Edit file"),
		mcp.WithDescription("Replace one exact, unique UTF-8 text occurrence in an open workspace file."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to the workspace root")),
		mcp.WithString("old_string", mcp.Required(), mcp.Description("Exact unique text to replace")),
		mcp.WithString("new_string", mcp.Required(), mcp.Description("Replacement text")),
		mcp.WithOutputSchema[diffView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(editTool, handleEdit)

	bashTool := mcp.NewTool("bash",
		mcp.WithTitleAnnotation("Run command"),
		mcp.WithDescription("Run a bounded shell command inside an open workspace. Small output is returned unchanged; large output is redacted, stored as a secure artifact, and represented by exact head/error/tail windows. Use artifact_read for omitted ranges. Do not modify files through shell redirection when write or edit can be used."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to run")),
		mcp.WithString("cwd", mcp.Description("Optional working directory relative to the workspace root")),
		mcp.WithNumber("timeout_seconds", mcp.Description("Optional timeout, default 120 and maximum 300 seconds")),
		mcp.WithOutputSchema[commandView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(bashTool, handleBash)

	artifactTool := mcp.NewTool("artifact_read",
		mcp.WithTitleAnnotation("Read artifact"),
		mcp.WithDescription("Read or search a secure Tautline artifact created from large command output. Artifact IDs are opaque, workspace-scoped, redacted before persistence, and cannot be used as filesystem paths."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier that created the artifact")),
		mcp.WithString("artifact_id", mcp.Required(), mcp.Description("Opaque artifact ID returned by bash")),
		mcp.WithNumber("start_line", mcp.Description("Optional first line to read")),
		mcp.WithNumber("end_line", mcp.Description("Optional last line to read")),
		mcp.WithNumber("tail", mcp.Description("Read the last N lines, maximum 2000")),
		mcp.WithNumber("max_lines", mcp.Description("Window size when end_line is omitted, default 200 and maximum 2000")),
		mcp.WithString("cursor", mcp.Description("Continuation cursor returned by an earlier artifact_read")),
		mcp.WithString("query", mcp.Description("Optional literal text or regular expression to search inside the artifact")),
		mcp.WithBoolean("regex", mcp.Description("Interpret query as a Go regular expression")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Use case-sensitive matching; default false")),
		mcp.WithNumber("context_lines", mcp.Description("Surrounding lines per search match, default 2 and maximum 12")),
		mcp.WithNumber("max_results", mcp.Description("Maximum search matches, default 20 and maximum 100")),
		mcp.WithOutputSchema[artifactView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(artifactTool, handleArtifactRead)

	if activeWidgetMode != widgetModeOff {
		showChangesTool := mcp.NewTool("show_changes",
			mcp.WithTitleAnnotation("Review changes"),
			mcp.WithDescription("Render one aggregate review after the final write or edit in the current turn. Call exactly once before the final response, not after every file change."),
			mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
			mcp.WithOutputSchema[changesModelView](),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		)
		s.AddTool(showChangesTool, handleShowChanges)
	}

	registerSkillTools(s)
	registerAgentTools(s)
}

func setWidgetMeta(tool *mcp.Tool, resourceURI, invoking, invoked string) {
	tool.Meta = mcp.NewMetaFromMap(map[string]any{
		"ui": map[string]any{
			"resourceUri": resourceURI,
			"visibility":  []string{"model", "app"},
		},
		"openai/outputTemplate":          resourceURI,
		"openai/toolInvocation/invoking": invoking,
		"openai/toolInvocation/invoked":  invoked,
		"openai/widgetAccessible":        true,
	})
}

func setAppOnlyToolMeta(tool *mcp.Tool) {
	tool.Meta = mcp.NewMetaFromMap(map[string]any{
		"ui": map[string]any{
			"visibility": []string{"app"},
		},
		"openai/visibility": "private",
	})
}

func newToolResult(toolName string, modelContent, activityContent any, fallback string) *mcp.CallToolResult {
	result := mcp.NewToolResultStructured(modelContent, fallback)
	if recordActivity(toolName, activityContent, fallback) {
		result.Meta = mcp.NewMetaFromMap(map[string]any{activityRecordedMeta: true})
	}
	return result
}

func handleActivitySnapshot(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	workspaceID := argStr(req, "workspace_id")
	if _, err := getWorkspace(workspaceID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	snapshot := runtime.activity.snapshot(workspaceID, argStr(req, "event_id"))
	return mcp.NewToolResultStructured(snapshot, snapshot.Summary), nil
}

func handleOpenWorkspace(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rp, err := resolvePath(argStr(req, "path"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	info, err := os.Stat(rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("not a directory: " + rp), nil
	}

	state := registerWorkspace(rp)
	files, stats, truncated, err := repositoryTree(rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	widgetView := workspaceView{
		Kind:        "workspace",
		Title:       filepath.Base(rp),
		Summary:     fmt.Sprintf("%d files · %d folders", stats.Files, stats.Directories),
		WorkspaceID: state.ID,
		Path:        rp,
		Files:       files,
		Stats:       stats,
		Truncated:   truncated,
	}
	modelFiles := files
	if len(modelFiles) > modelWorkspaceEntries {
		modelFiles = modelFiles[:modelWorkspaceEntries]
	}
	modelView := workspaceModelView{
		Kind:        widgetView.Kind,
		Title:       widgetView.Title,
		Summary:     widgetView.Summary,
		WorkspaceID: widgetView.WorkspaceID,
		Path:        widgetView.Path,
		Files:       modelFiles,
		Stats:       widgetView.Stats,
		Truncated:   widgetView.Truncated || len(files) > len(modelFiles),
	}
	fallback := fmt.Sprintf("Workspace %s opened as %s · %d files · %d folders.", filepath.Base(rp), state.ID, stats.Files, stats.Directories)
	return newToolResult("open_workspace", modelView, widgetView, fallback), nil
}

func handleRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	view, err := readWorkspaceFileWindow(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fallback := fmt.Sprintf("Read %s lines %d-%d of %d.", view.Path, view.StartLine, view.EndLine, view.TotalLines)
	if view.Truncated {
		fallback += " Additional content remains available."
	}
	return newToolResult("read", view, view, fallback), nil
}

func handleWrite(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := argStr(req, "workspace_id")
	state, rp, err := resolveWorkspacePath(workspaceID, argStr(req, "path"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	after := argStr(req, "content")
	if !utf8.ValidString(after) {
		return mcp.NewToolResultError("content must be valid UTF-8 text"), nil
	}
	before, mode, err := existingTextAndMode(rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	_, statErr := os.Stat(rp)
	existed := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return mcp.NewToolResultError(statErr.Error()), nil
	}
	if activeWidgetMode != widgetModeOff {
		if err := state.recordOriginal(rp, fileSnapshot{Exists: existed, Content: before}); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}
	if err := atomicWriteFile(rp, []byte(after), mode); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	relative, _ := state.relativePath(rp)
	diff, added, removed, truncated := unifiedDiff(relative, before, after)
	view := diffView{
		Kind:        "write",
		Title:       "Written " + filepath.Base(rp),
		Summary:     fmt.Sprintf("+%d −%d", added, removed),
		WorkspaceID: workspaceID,
		Path:        relative,
		Operation:   "write",
		Diff:        diff,
		Stats:       toolStats{Bytes: int64(len(after)), Lines: countLines(after), Added: added, Removed: removed},
		Truncated:   truncated,
	}
	return newToolResult("write", view, view, fmt.Sprintf("Written %s · +%d −%d.", relative, added, removed)), nil
}

func handleEdit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := argStr(req, "workspace_id")
	state, rp, err := resolveWorkspacePath(workspaceID, argStr(req, "path"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	beforeBytes, err := os.ReadFile(rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if bytes.IndexByte(beforeBytes, 0) >= 0 || !utf8.Valid(beforeBytes) {
		return mcp.NewToolResultError("edit only supports UTF-8 text files"), nil
	}
	oldS, newS := argStr(req, "old_string"), argStr(req, "new_string")
	if oldS == "" {
		return mcp.NewToolResultError("old_string must not be empty"), nil
	}
	before := string(beforeBytes)
	occurrences := strings.Count(before, oldS)
	if occurrences == 0 {
		return mcp.NewToolResultError("old_string not found"), nil
	}
	if occurrences > 1 {
		return mcp.NewToolResultError(fmt.Sprintf("old_string is not unique: found %d occurrences", occurrences)), nil
	}
	after := strings.Replace(before, oldS, newS, 1)
	info, err := os.Stat(rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if activeWidgetMode != widgetModeOff {
		if err := state.recordOriginal(rp, fileSnapshot{Exists: true, Content: before}); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}
	if err := atomicWriteFile(rp, []byte(after), info.Mode().Perm()); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	relative, _ := state.relativePath(rp)
	diff, added, removed, truncated := unifiedDiff(relative, before, after)
	view := diffView{
		Kind:        "edit",
		Title:       "Edited " + filepath.Base(rp),
		Summary:     fmt.Sprintf("+%d −%d", added, removed),
		WorkspaceID: workspaceID,
		Path:        relative,
		Operation:   "edit",
		Diff:        diff,
		Stats:       toolStats{Bytes: int64(len(after)), Lines: countLines(after), Added: added, Removed: removed, Occurrences: occurrences},
		Truncated:   truncated,
	}
	return newToolResult("edit", view, view, fmt.Sprintf("Edited %s · +%d −%d.", relative, added, removed)), nil
}

func handleBash(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := argStr(req, "workspace_id")
	command := strings.TrimSpace(argStr(req, "command"))
	if command == "" {
		return mcp.NewToolResultError("empty command"), nil
	}
	_, cwd, err := resolveWorkspaceDirectory(workspaceID, argStr(req, "cwd"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	timeoutSeconds := clampInt(argInt(req, "timeout_seconds", defaultCommandSecs), 1, maxCommandSecs)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	start := time.Now()
	capture, exitCode, runErr := runShellCaptured(runCtx, workspaceID, cwd, command)
	duration := time.Since(start)
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		exitCode = 124
	} else if errors.Is(runCtx.Err(), context.Canceled) {
		exitCode = 130
	} else if runErr != nil && capture.Output == "" {
		capture.Output = runErr.Error()
	}

	state, _ := getWorkspace(workspaceID)
	relativeCWD, _ := state.relativePath(cwd)
	if relativeCWD == "" {
		relativeCWD = "."
	}
	view := commandView{
		Kind:               "command",
		Title:              "Command finished",
		Summary:            fmt.Sprintf("Exit %d · %s", exitCode, duration.Round(time.Millisecond)),
		WorkspaceID:        workspaceID,
		Path:               relativeCWD,
		Command:            command,
		Output:             capture.Output,
		Success:            exitCode == 0,
		Stats:              toolStats{DurationMS: duration.Milliseconds(), ExitCode: intPointer(exitCode), Bytes: capture.OriginalBytes, Lines: capture.TotalLines},
		Truncated:          capture.CaptureTruncated || capture.OmittedLines > 0,
		Artifact:           capture.Artifact,
		IncludedRanges:     capture.IncludedRanges,
		OmittedLines:       capture.OmittedLines,
		Redacted:           capture.Redacted,
		StorageUnavailable: capture.StorageUnavailable,
	}
	fallback := fmt.Sprintf("Exit %d · %s.", exitCode, duration.Round(time.Millisecond))
	if capture.Artifact != nil {
		fallback += fmt.Sprintf(" Large output stored as %s; %d lines omitted from the inline result.", capture.Artifact.ID, capture.OmittedLines)
	} else if capture.CaptureTruncated {
		fallback += " Output reached the configured safety limit."
	}
	if capture.Redacted {
		fallback += " Secret-looking values were redacted."
	}
	return newToolResult("bash", view, view, fallback), nil
}

func handleShowChanges(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := argStr(req, "workspace_id")
	state, err := getWorkspace(workspaceID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	model, widget, err := state.buildChangeReview()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return newToolResult("show_changes", model, widget, model.Summary+"."), nil
}

func intPointer(value int) *int {
	return &value
}

func languageFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".jsx":
		return "javascript-react"
	case ".json":
		return "json"
	case ".md", ".mdx":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".py":
		return "python"
	case ".sh", ".bash":
		return "shell"
	case ".cmd", ".bat":
		return "batch"
	case ".yaml", ".yml":
		return "yaml"
	case ".xml":
		return "xml"
	default:
		return "text"
	}
}

func repositoryTree(root string) ([]viewFile, toolStats, bool, error) {
	entries := make([]viewFile, 0, 128)
	stats := toolStats{}
	truncated := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1
		if entry.IsDir() && shouldSkipDir(entry.Name()) {
			return filepath.SkipDir
		}
		if depth > maxWorkspaceDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(entries) >= maxWorkspaceEntries {
			truncated = true
			return filepath.SkipAll
		}

		item := viewFile{Name: entry.Name(), Path: filepath.ToSlash(rel)}
		if entry.IsDir() {
			item.Type = "dir"
			stats.Directories++
		} else {
			item.Type = "file"
			stats.Files++
			if info, err := entry.Info(); err == nil {
				item.Size = info.Size()
				stats.Bytes += info.Size()
			}
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return nil, toolStats{}, false, err
	}
	stats.Entries = len(entries)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "dir"
		}
		return strings.ToLower(entries[i].Path) < strings.ToLower(entries[j].Path)
	})
	return entries, stats, truncated, nil
}

func shouldSkipDir(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "backup-") {
		return true
	}
	switch lower {
	case ".git", ".idea", ".vscode", "node_modules", "vendor", "dist", "build", "bin", "coverage", "tmp", "temp":
		return true
	default:
		return false
	}
}

func readTextBounded(path string, limit int) (string, bool, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, nil, err
	}
	if info.IsDir() {
		return "", false, nil, fmt.Errorf("not a file: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit+1)))
	if err != nil {
		return "", false, nil, err
	}
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", false, nil, fmt.Errorf("file is binary or not valid UTF-8: %s", path)
	}
	return string(data), truncated, info, nil
}

func existingTextAndMode(path string) (string, fs.FileMode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0o644, nil
		}
		return "", 0, err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", 0, fmt.Errorf("existing file is binary or not valid UTF-8: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	return string(data), info.Mode().Perm(), nil
}

func atomicWriteFile(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tautline-write-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("atomic replace failed: %w", err)
	}
	return nil
}

func unifiedDiff(name, before, after string) (string, int, int, bool) {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(before),
		B:        difflib.SplitLines(after),
		FromFile: "a/" + name,
		ToFile:   "b/" + name,
		Context:  3,
	})
	if err != nil {
		diff = fmt.Sprintf("diff unavailable: %v", err)
	}
	added, removed := diffStats(diff)
	trimmed, truncated := truncateUTF8(diff, maxCommandBytes)
	return trimmed, added, removed, truncated
}

func diffStats(diff string) (added, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			removed++
		}
	}
	return added, removed
}

func runShell(ctx context.Context, cwd, command string) (string, int, bool, error) {
	var cmd *exec.Cmd
	if configured := strings.TrimSpace(os.Getenv("DEVSPACE_SHELL")); configured != "" {
		cmd = exec.Command(configured, "-c", command)
	} else if runtime.GOOS == "windows" {
		gitBash := `C:\Program Files\Git\bin\bash.exe`
		if _, err := os.Stat(gitBash); err == nil {
			cmd = exec.Command(gitBash, "-c", command)
		} else {
			cmd = exec.Command("cmd.exe", "/d", "/s", "/c", command)
		}
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = cwd
	buffer := &boundedBuffer{limit: maxCommandBytes}
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	if err := cmd.Start(); err != nil {
		return "", -1, false, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		killProcessTree(cmd.Process.Pid)
		waitErr = <-done
	}

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return strings.TrimRight(buffer.String(), "\r\n"), exitCode, buffer.Truncated(), waitErr
}

func killProcessTree(pid int) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T", "/F").Run()
		return
	}
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Kill()
	}
}

type boundedBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	remaining := b.limit - len(b.data)
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(p) > remaining {
		b.data = append(b.data, p[:remaining]...)
		b.truncated = true
		return original, nil
	}
	b.data = append(b.data, p...)
	return original, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := append([]byte(nil), b.data...)
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func truncateUTF8(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	data := []byte(value)[:limit]
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data) + "\n… output truncated …", true
}

func countLines(value string) int {
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}

func withLineNumbers(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	width := len(fmt.Sprintf("%d", len(lines)))
	var builder strings.Builder
	for index, line := range lines {
		fmt.Fprintf(&builder, "%*d  %s", width, index+1, line)
		if index < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func argStr(req mcp.CallToolRequest, key string) string {
	args := req.GetArguments()
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func argInt(req mcp.CallToolRequest, key string, fallback int) int {
	args := req.GetArguments()
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case float32:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	default:
		return fallback
	}
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
