package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const maxReadManyFiles = 10

type readManyError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type readManyView struct {
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	WorkspaceID string          `json:"workspaceId"`
	Files       []fileView      `json:"files"`
	Errors      []readManyError `json:"errors,omitempty"`
	Stats       toolStats       `json:"stats"`
	Truncated   bool            `json:"truncated,omitempty"`
}

func registerReadManyTool(s *server.MCPServer) {
	fileSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path":            map[string]any{"type": "string", "description": "File path relative to the workspace root"},
			"start_line":      map[string]any{"type": "integer"},
			"end_line":        map[string]any{"type": "integer"},
			"head":            map[string]any{"type": "integer"},
			"tail":            map[string]any{"type": "integer"},
			"max_lines":       map[string]any{"type": "integer", "minimum": 1, "maximum": maxReadWindowLines},
			"cursor":          map[string]any{"type": "string"},
			"expected_sha256": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
	tool := mcp.NewTool("read_many",
		mcp.WithTitleAnnotation("Read multiple files"),
		mcp.WithDescription("Read up to 10 UTF-8 workspace files in one bounded call. Each item uses the same path validation, line windows, cursors, freshness checks, hashes, and truncation behavior as read."),
		mcp.WithString("workspace_id", mcp.Required(), mcp.Description("Workspace identifier returned by open_workspace")),
		mcp.WithArray("files", mcp.Required(), mcp.Description("One to ten file read requests"), mcp.Items(fileSchema)),
		mcp.WithNumber("max_bytes", mcp.Description("Strict aggregate structured response limit, default 65536 and maximum 131072 bytes")),
		mcp.WithOutputSchema[readManyView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(tool, handleReadMany)
}

func handleReadMany(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := strings.TrimSpace(argStr(req, "workspace_id"))
	if _, err := getWorkspace(workspaceID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	rawFiles, ok := req.GetArguments()["files"].([]any)
	if !ok || len(rawFiles) == 0 {
		return mcp.NewToolResultError("files must contain at least one file read request"), nil
	}
	if len(rawFiles) > maxReadManyFiles {
		return mcp.NewToolResultError(fmt.Sprintf("files contains %d entries; maximum is %d", len(rawFiles), maxReadManyFiles)), nil
	}
	limit := clampInt(argInt(req, "max_bytes", defaultReadBytes), 8*1024, maxSearchOutputBytes)
	perFileBytes := clampInt(limit/len(rawFiles), 1024, maxReadBytes)
	view := readManyView{
		Kind:        "files",
		Title:       "Workspace files",
		WorkspaceID: workspaceID,
		Files:       make([]fileView, 0, len(rawFiles)),
	}
	for index, raw := range rawFiles {
		item, ok := raw.(map[string]any)
		if !ok {
			view.Errors = append(view.Errors, readManyError{Path: fmt.Sprintf("item %d", index+1), Error: "file request must be an object"})
			continue
		}
		path := strings.TrimSpace(mapString(item, "path"))
		if path == "" {
			view.Errors = append(view.Errors, readManyError{Path: fmt.Sprintf("item %d", index+1), Error: "path is required"})
			continue
		}
		arguments := map[string]any{"workspace_id": workspaceID, "path": path, "max_bytes": perFileBytes}
		for _, key := range []string{"start_line", "end_line", "head", "tail", "max_lines", "cursor", "expected_sha256"} {
			if value, exists := item[key]; exists {
				arguments[key] = value
			}
		}
		readRequest := req
		readRequest.Params.Name = "read"
		readRequest.Params.Arguments = arguments
		readRequest.Params.RawArguments = nil
		file, err := readWorkspaceFileWindow(readRequest)
		if err != nil {
			view.Errors = append(view.Errors, readManyError{Path: path, Error: err.Error()})
			continue
		}
		view.Truncated = view.Truncated || file.Truncated
		view.Files = append(view.Files, file)
	}
	view.Summary = fmt.Sprintf("%d files read", len(view.Files))
	if len(view.Errors) > 0 {
		view.Summary += fmt.Sprintf(" · %d errors", len(view.Errors))
	}
	boundReadManyView(&view, limit)
	fallback := fmt.Sprintf("Read %d files from workspace %s.", len(view.Files), workspaceID)
	if len(view.Errors) > 0 {
		fallback += fmt.Sprintf(" %d requests failed.", len(view.Errors))
	}
	if view.Truncated {
		fallback += " Output was bounded."
	}
	return newToolResult("read_many", view, view, fallback), nil
}

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func boundReadManyView(view *readManyView, limit int) {
	for {
		recalculateReadManyStats(view)
		encoded, err := json.Marshal(view)
		if err != nil || len(encoded) <= limit {
			return
		}
		excess := len(encoded) - limit
		trimmed := false
		for index := len(view.Files) - 1; index >= 0; index-- {
			if view.Files[index].Content == "" {
				continue
			}
			target := len(view.Files[index].Content) - excess - 256
			if target < 0 {
				target = 0
			}
			view.Files[index] = trimReadManyFile(view.Files[index], target)
			view.Truncated = true
			trimmed = true
			break
		}
		if !trimmed {
			return
		}
	}
}

func trimReadManyFile(view fileView, budget int) fileView {
	originalStart := view.StartLine
	if budget <= 0 {
		view.Content = ""
		view.EndLine = 0
		view.Stats.Lines = 0
		if originalStart > 0 && originalStart <= view.TotalLines {
			view.NextCursor = makeLineCursor(originalStart, view.SHA256)
		}
		view.Truncated = true
		view.Summary = fmt.Sprintf("content omitted by aggregate limit · %d total lines", view.TotalLines)
		return view
	}
	prefix := truncateUTF8Prefix(view.Content, budget)
	if cut := strings.LastIndexByte(prefix, '\n'); cut >= 0 {
		prefix = prefix[:cut]
	}
	prefix = strings.TrimSuffix(prefix, "\r")
	shown := 0
	if prefix != "" {
		shown = strings.Count(prefix, "\n") + 1
	}
	view.Content = prefix
	view.Stats.Lines = shown
	if shown == 0 {
		view.EndLine = 0
		if originalStart > 0 && originalStart <= view.TotalLines {
			view.NextCursor = makeLineCursor(originalStart, view.SHA256)
		}
	} else {
		view.EndLine = originalStart + shown - 1
		if view.EndLine < view.TotalLines {
			view.NextCursor = makeLineCursor(view.EndLine+1, view.SHA256)
		}
	}
	view.Truncated = true
	view.Summary = fmt.Sprintf("lines %d-%d of %d · aggregate limit", view.StartLine, view.EndLine, view.TotalLines)
	return view
}

func recalculateReadManyStats(view *readManyView) {
	view.Stats = toolStats{Files: len(view.Files)}
	for _, file := range view.Files {
		view.Stats.Bytes += int64(len(file.Content))
		view.Stats.Lines += file.Stats.Lines
	}
}
