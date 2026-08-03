package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestActivityMiddlewareRecordsBuiltInAndDynamicTools(t *testing.T) {
	store := newActivityStore()
	middleware := activityMiddleware(store)
	handler := middleware(func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if request.Params.Name == "remote_fail" {
			return nil, errors.New("remote unavailable")
		}
		return mcp.NewToolResultText("remote document updated"), nil
	})

	request := toolRequest("gdocs_update_document", map[string]any{"document_id": "doc-1"})
	if _, err := handler(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), toolRequest("remote_fail", map[string]any{})); err == nil {
		t.Fatal("protocol error was not returned")
	}
	snapshot := store.snapshot("ws_unused", "")
	if len(snapshot.Events) != 2 {
		t.Fatalf("middleware recorded %d events, want 2", len(snapshot.Events))
	}
	if snapshot.Events[0].Tool != "remote_fail" || snapshot.Events[0].Status != "error" {
		t.Fatalf("failed dynamic tool was not recorded correctly: %+v", snapshot.Events[0])
	}
	if snapshot.Events[1].Tool != "gdocs_update_document" || snapshot.Events[1].Summary != "remote document updated" {
		t.Fatalf("successful dynamic tool was not recorded correctly: %+v", snapshot.Events[1])
	}
}

func TestActivityMiddlewareRemovesInternalMarker(t *testing.T) {
	store := newActivityStore()
	result := mcp.NewToolResultText("already recorded")
	result.Meta = mcp.NewMetaFromMap(map[string]any{activityRecordedMeta: true})
	handler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result, nil
	})
	returned, err := handler(context.Background(), toolRequest("write", map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if returned.Meta != nil || len(store.events) != 0 {
		t.Fatalf("internal marker leaked or event was duplicated: meta=%+v events=%+v", returned.Meta, store.events)
	}
}

func TestActivityMiddlewareSkipsSnapshotPolling(t *testing.T) {
	store := newActivityStore()
	handler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("snapshot"), nil
	})
	if _, err := handler(context.Background(), toolRequest("activity_snapshot", map[string]any{})); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 0 {
		t.Fatalf("snapshot polling created recursive activity: %+v", store.events)
	}
}

func TestActivityStoreFiltersWorkspaceAndKeepsGlobalEvents(t *testing.T) {
	store := newActivityStore()
	store.record("skills_search", map[string]any{
		"kind":    "skills_search",
		"title":   "Skills matched",
		"summary": "3 skills",
	}, "skills", false)
	store.record("write", map[string]any{
		"kind":        "write",
		"workspaceId": "ws_alpha",
		"path":        "alpha.go",
		"summary":     "alpha updated",
		"stats":       map[string]any{"added": 2, "removed": 1},
	}, "alpha", false)
	store.record("write", map[string]any{
		"kind":        "write",
		"workspaceId": "ws_beta",
		"path":        "beta.go",
		"summary":     "beta updated",
	}, "beta", false)

	snapshot := store.snapshot("ws_alpha", "")
	if len(snapshot.Events) != 2 {
		t.Fatalf("workspace snapshot has %d events, want 2: %+v", len(snapshot.Events), snapshot.Events)
	}
	if snapshot.Events[0].Path != "alpha.go" || snapshot.Events[1].Kind != "skills_search" {
		t.Fatalf("unexpected event order or filtering: %+v", snapshot.Events)
	}
	if snapshot.Selected == nil || snapshot.Selected.ID != snapshot.Events[0].ID {
		t.Fatalf("latest event was not selected: %+v", snapshot.Selected)
	}
}

func TestActivityStoreCanInspectOlderEvent(t *testing.T) {
	store := newActivityStore()
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_one", "path": "one.go", "content": "one"}, "one", false)
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_one", "path": "two.go", "content": "two"}, "two", false)
	latest := store.snapshot("ws_one", "")
	olderID := latest.Events[1].ID
	selected := store.snapshot("ws_one", olderID)
	if selected.Selected == nil || selected.Selected.ID != olderID {
		t.Fatalf("event %s was not selected: %+v", olderID, selected.Selected)
	}
	if !bytes.Contains(selected.Selected.Payload, []byte(`"content":"one"`)) {
		t.Fatalf("selected payload is incorrect: %s", selected.Selected.Payload)
	}
}

func TestActivityPayloadIsRedactedAndBounded(t *testing.T) {
	secret := "supersecretvalue123456"
	payload := map[string]any{
		"kind":        "command",
		"workspaceId": "ws_safe",
		"command":     "run --token=" + secret,
		"output":      strings.Repeat("line\n", activityTextBytes) + "api_key=" + secret,
	}
	encoded, fields := compactActivityPayload(payload)
	if len(encoded) > activityPayloadBytes {
		t.Fatalf("payload is too large: %d", len(encoded))
	}
	if bytes.Contains(encoded, []byte(secret)) || !bytes.Contains(encoded, []byte("[REDACTED]")) {
		t.Fatalf("payload was not redacted: %s", encoded)
	}
	if fields["kind"] != "command" {
		t.Fatalf("payload fields were lost: %+v", fields)
	}
}

func TestActivityStoreKeepsOnlyRecentEvents(t *testing.T) {
	store := newActivityStore()
	for index := 0; index < activityLimit+12; index++ {
		store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_limit", "path": index}, "read", false)
	}
	store.mu.RLock()
	count := len(store.events)
	firstSequence := store.events[0].Sequence
	lastSequence := store.events[len(store.events)-1].Sequence
	store.mu.RUnlock()
	if count != activityLimit || firstSequence != 13 || lastSequence != activityLimit+12 {
		t.Fatalf("ring buffer count=%d first=%d last=%d", count, firstSequence, lastSequence)
	}
}

func TestActivitySnapshotJSONCarriesOneDetailedPayload(t *testing.T) {
	store := newActivityStore()
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_json", "path": "one.go", "content": "one"}, "one", false)
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_json", "path": "two.go", "content": "two"}, "two", false)
	encoded, err := json.Marshal(store.snapshot("ws_json", ""))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(encoded, []byte(`"payload"`)) != 1 {
		t.Fatalf("snapshot should contain one detailed payload: %s", encoded)
	}
}
