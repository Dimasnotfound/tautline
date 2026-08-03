package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	activityRecordedMeta = "tautline/activityRecorded"
	activityLimit        = 64
	activitySnapshotSize = 28
	activityPayloadBytes = 64 * 1024
	activityTextBytes    = 40 * 1024
	activityArrayItems   = 40
)

type activityEvent struct {
	ID          string          `json:"id"`
	Sequence    uint64          `json:"sequence"`
	Tool        string          `json:"tool"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary,omitempty"`
	Status      string          `json:"status"`
	WorkspaceID string          `json:"workspaceId,omitempty"`
	Path        string          `json:"path,omitempty"`
	OccurredAt  time.Time       `json:"occurredAt"`
	Stats       map[string]any  `json:"stats,omitempty"`
	Payload     json.RawMessage `json:"-"`
}

type activityEventView struct {
	ID          string         `json:"id"`
	Sequence    uint64         `json:"sequence"`
	Tool        string         `json:"tool"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary,omitempty"`
	Status      string         `json:"status"`
	WorkspaceID string         `json:"workspaceId,omitempty"`
	Path        string         `json:"path,omitempty"`
	OccurredAt  time.Time      `json:"occurredAt"`
	Stats       map[string]any `json:"stats,omitempty"`
}

type activitySelection struct {
	activityEventView
	Payload json.RawMessage `json:"payload,omitempty"`
}

type activitySnapshot struct {
	Kind          string              `json:"kind"`
	Title         string              `json:"title"`
	Summary       string              `json:"summary"`
	WorkspaceID   string              `json:"workspaceId"`
	WorkspacePath string              `json:"workspacePath,omitempty"`
	Sequence      uint64              `json:"sequence"`
	UpdatedAt     time.Time           `json:"updatedAt"`
	Events        []activityEventView `json:"events"`
	Selected      *activitySelection  `json:"selected,omitempty"`
}

type activityStore struct {
	mu       sync.RWMutex
	sequence uint64
	events   []activityEvent
}

func newActivityStore() *activityStore {
	return &activityStore{events: make([]activityEvent, 0, activityLimit)}
}

func isInternalActivityTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "tautline_activity", "activity_snapshot", "workspace_lookup":
		return true
	default:
		return false
	}
}

func recordActivity(toolName string, payload any, fallback string) bool {
	runtime, err := currentApplicationRuntime()
	if err != nil || runtime.activity == nil || isInternalActivityTool(toolName) {
		return false
	}
	runtime.activity.record(toolName, payload, fallback, false)
	return true
}

func consumeActivityMarker(result *mcp.CallToolResult) bool {
	if result == nil || result.Meta == nil || result.Meta.AdditionalFields == nil {
		return false
	}
	marked, _ := result.Meta.AdditionalFields[activityRecordedMeta].(bool)
	if !marked {
		return false
	}
	delete(result.Meta.AdditionalFields, activityRecordedMeta)
	if len(result.Meta.AdditionalFields) == 0 && result.Meta.ProgressToken == nil {
		result.Meta = nil
	}
	return true
}

func activityMiddleware(store *activityStore) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := next(ctx, request)
			if store == nil || isInternalActivityTool(request.Params.Name) || consumeActivityMarker(result) {
				return result, err
			}
			payload, fallback := activityCallPayload(request, result, err)
			store.record(request.Params.Name, payload, fallback, err != nil || result != nil && result.IsError)
			return result, err
		}
	}
}

func activityCallPayload(request mcp.CallToolRequest, result *mcp.CallToolResult, callErr error) (map[string]any, string) {
	fallback := ""
	if callErr != nil {
		fallback = callErr.Error()
	} else if result != nil {
		fallback = activityResultText(result)
	}
	status := "complete"
	if callErr != nil || result != nil && result.IsError {
		status = "error"
	}
	kind := normalizeMCPToken(request.Params.Name)
	payload := map[string]any{
		"kind":    kind,
		"title":   activityTitle(request.Params.Name, kind, status),
		"summary": fallback,
	}
	if result != nil && result.StructuredContent != nil {
		if structured := activityMap(result.StructuredContent); structured != nil {
			payload = structured
		}
	}
	arguments := activityMap(request.Params.Arguments)
	if workspaceIDFromFields(payload) == "" {
		if workspaceID := workspaceIDFromFields(arguments); workspaceID != "" {
			payload["workspaceId"] = workspaceID
		}
	}
	if _, exists := payload["arguments"]; !exists && len(arguments) > 0 && result != nil && result.StructuredContent == nil {
		payload["arguments"] = arguments
	}
	if callErr != nil || result != nil && result.IsError {
		payload["status"] = "error"
	}
	return payload, fallback
}

func activityMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil
	}
	return fields
}

func activityResultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if text, ok := mcp.AsTextContent(content); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, strings.TrimSpace(text.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func (store *activityStore) record(toolName string, payload any, fallback string, failed bool) {
	compact, fields := compactActivityPayload(payload)
	kind := stringField(fields, "kind")
	if kind == "" {
		kind = normalizeMCPToken(toolName)
	}
	workspaceID := workspaceIDFromFields(fields)
	status := activityStatus(fields, failed)
	title := stringField(fields, "title")
	if title == "" {
		title = activityTitle(toolName, kind, status)
	}
	summary := stringField(fields, "summary")
	if summary == "" {
		summary = strings.TrimSpace(fallback)
	}
	path := firstStringField(fields, "path", "url", "command", "file", "query")
	stats, _ := fields["stats"].(map[string]any)

	store.mu.Lock()
	store.sequence++
	event := activityEvent{
		ID:          fmt.Sprintf("event_%d", store.sequence),
		Sequence:    store.sequence,
		Tool:        toolName,
		Kind:        kind,
		Title:       title,
		Summary:     summary,
		Status:      status,
		WorkspaceID: workspaceID,
		Path:        path,
		OccurredAt:  time.Now().UTC(),
		Stats:       cloneActivityMap(stats),
		Payload:     compact,
	}
	store.events = append(store.events, event)
	if len(store.events) > activityLimit {
		copy(store.events, store.events[len(store.events)-activityLimit:])
		store.events = store.events[:activityLimit]
	}
	store.mu.Unlock()
}

func (store *activityStore) snapshot(workspaceID, eventID string) activitySnapshot {
	workspaceID = strings.TrimSpace(workspaceID)
	eventID = strings.TrimSpace(eventID)
	workspacePath := ""
	if workspace, err := getWorkspace(workspaceID); err == nil {
		workspacePath = workspace.Root
	}

	store.mu.RLock()
	sequence := store.sequence
	filtered := make([]activityEvent, 0, activitySnapshotSize)
	for index := len(store.events) - 1; index >= 0 && len(filtered) < activitySnapshotSize; index-- {
		event := store.events[index]
		if event.WorkspaceID == "" || event.WorkspaceID == workspaceID {
			filtered = append(filtered, event)
		}
	}
	store.mu.RUnlock()

	views := make([]activityEventView, 0, len(filtered))
	for _, event := range filtered {
		views = append(views, event.view())
	}
	selectedIndex := 0
	if eventID != "" {
		for index, event := range filtered {
			if event.ID == eventID {
				selectedIndex = index
				break
			}
		}
	}
	var selected *activitySelection
	if len(filtered) > 0 {
		event := filtered[selectedIndex]
		selected = &activitySelection{activityEventView: event.view(), Payload: append(json.RawMessage(nil), event.Payload...)}
	}
	summary := "No activity yet"
	updatedAt := time.Time{}
	if len(filtered) > 0 {
		summary = filtered[0].Summary
		if summary == "" {
			summary = filtered[0].Title
		}
		updatedAt = filtered[0].OccurredAt
	}
	return activitySnapshot{
		Kind:          "activity_snapshot",
		Title:         "Tautline activity",
		Summary:       summary,
		WorkspaceID:   workspaceID,
		WorkspacePath: workspacePath,
		Sequence:      sequence,
		UpdatedAt:     updatedAt,
		Events:        views,
		Selected:      selected,
	}
}

func (event activityEvent) view() activityEventView {
	return activityEventView{
		ID:          event.ID,
		Sequence:    event.Sequence,
		Tool:        event.Tool,
		Kind:        event.Kind,
		Title:       event.Title,
		Summary:     event.Summary,
		Status:      event.Status,
		WorkspaceID: event.WorkspaceID,
		Path:        event.Path,
		OccurredAt:  event.OccurredAt,
		Stats:       cloneActivityMap(event.Stats),
	}
}

func compactActivityPayload(value any) (json.RawMessage, map[string]any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		fallback := map[string]any{"summary": fmt.Sprintf("Payload could not be encoded: %v", err)}
		encoded, _ = json.Marshal(fallback)
		return encoded, fallback
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		fallback := map[string]any{"summary": "Payload could not be decoded"}
		encoded, _ = json.Marshal(fallback)
		return encoded, fallback
	}
	decoded = compactActivityValue(decoded, "")
	fields, _ := decoded.(map[string]any)
	if fields == nil {
		fields = map[string]any{"value": decoded}
		decoded = fields
	}
	encoded, _ = json.Marshal(decoded)
	if len(encoded) > activityPayloadBytes {
		fields = activitySummaryFields(fields)
		encoded, _ = json.Marshal(fields)
	}
	return encoded, fields
}

func compactActivityValue(value any, key string) any {
	switch current := value.(type) {
	case string:
		redacted, _ := redactSensitiveText(current)
		limit := activityTextBytes
		if key != "content" && key != "output" && key != "diff" && key != "excerpt" {
			limit = 4096
		}
		trimmed, truncated := truncateUTF8(redacted, limit)
		if truncated {
			return trimmed + "\n… output shortened for the activity monitor"
		}
		return trimmed
	case []any:
		limit := len(current)
		if limit > activityArrayItems {
			limit = activityArrayItems
		}
		items := make([]any, 0, limit)
		for index := 0; index < limit; index++ {
			items = append(items, compactActivityValue(current[index], key))
		}
		return items
	case map[string]any:
		result := make(map[string]any, len(current))
		for name, item := range current {
			result[name] = compactActivityValue(item, name)
		}
		return result
	default:
		return current
	}
}

func activitySummaryFields(fields map[string]any) map[string]any {
	result := map[string]any{"detailOmitted": true}
	for _, key := range []string{"kind", "title", "summary", "workspaceId", "workspace_id", "path", "url", "command", "query", "file", "stats", "files", "matches", "run", "success", "empty", "truncated"} {
		if value, exists := fields[key]; exists {
			result[key] = value
		}
	}
	return result
}

func workspaceIDFromFields(fields map[string]any) string {
	if value := firstStringField(fields, "workspaceId", "workspace_id"); value != "" {
		return value
	}
	if run, ok := fields["run"].(map[string]any); ok {
		return firstStringField(run, "workspaceId", "workspace_id")
	}
	return ""
}

func activityStatus(fields map[string]any, failed bool) string {
	if failed {
		return "error"
	}
	if success, exists := fields["success"].(bool); exists && !success {
		return "error"
	}
	if run, ok := fields["run"].(map[string]any); ok {
		if status := stringField(run, "status"); status != "" {
			return status
		}
	}
	if status := stringField(fields, "status"); status != "" {
		return status
	}
	return "complete"
}

func activityTitle(toolName, kind, status string) string {
	if status == "error" || status == "failed" {
		return humanActivityName(toolName) + " failed"
	}
	switch kind {
	case "workspace":
		return "Workspace opened"
	case "search":
		return "Workspace searched"
	case "file":
		return "File read"
	case "write":
		return "File written"
	case "edit":
		return "File edited"
	case "command":
		return "Command finished"
	case "show_changes":
		return "Changes reviewed"
	case "skills_search":
		return "Skills matched"
	case "skill":
		return "Skill loaded"
	case "skill_file":
		return "Skill file read"
	case "agent_run":
		return "Sub-agent updated"
	case "browser":
		return "Browser action finished"
	default:
		return humanActivityName(toolName)
	}
}

func humanActivityName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return "Tautline activity"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func firstStringField(fields map[string]any, names ...string) string {
	for _, name := range names {
		if value := stringField(fields, name); value != "" {
			return value
		}
	}
	return ""
}

func stringField(fields map[string]any, name string) string {
	value, _ := fields[name].(string)
	return strings.TrimSpace(value)
}

func cloneActivityMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
