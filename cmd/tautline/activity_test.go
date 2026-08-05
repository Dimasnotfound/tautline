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

func mustActivitySnapshot(t *testing.T, store *activityStore, monitorID, eventID string) activitySnapshot {
	t.Helper()
	snapshot, found := store.snapshotMonitor(monitorID, eventID)
	if !found {
		t.Fatalf("monitor %q was not found", monitorID)
	}
	return snapshot
}

func TestActivityMiddlewareRecordsBuiltInAndDynamicTools(t *testing.T) {
	store := newActivityStore()
	monitorID := store.startMonitor("")
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
	snapshot := mustActivitySnapshot(t, store, monitorID, "")
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
	monitorID := store.startMonitor("")
	result := mcp.NewToolResultText("already recorded")
	result.Meta = mcp.NewMetaFromMap(map[string]any{activityRecordedMeta: true})
	handler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result, nil
	})
	returned, err := handler(context.Background(), toolRequest("write", map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if returned.Meta != nil || len(mustActivitySnapshot(t, store, monitorID, "").Events) != 0 {
		t.Fatalf("internal marker leaked or event was duplicated: meta=%+v", returned.Meta)
	}
}

func TestActivityMiddlewareSkipsInternalMonitorTools(t *testing.T) {
	store := newActivityStore()
	handler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("internal"), nil
	})
	for _, toolName := range []string{"tautline_activity", "activity_snapshot", "workspace_lookup"} {
		if _, err := handler(context.Background(), toolRequest(toolName, map[string]any{})); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.RLock()
	monitorCount := len(store.monitors)
	store.mu.RUnlock()
	if monitorCount != 0 {
		t.Fatalf("internal monitor tools created recursive or noisy monitors: %d", monitorCount)
	}
}

func TestSkillsSearchStartsNewPromptMonitorAndBootstrapsWidget(t *testing.T) {
	isolateWorkspaceRegistry(t, nil)
	store := newActivityStore()
	previousID := store.startMonitor("")
	store.record("read", map[string]any{"kind": "file", "path": "old.go"}, "old prompt", false)

	handler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return newToolResult("skills_search", map[string]any{"kind": "skills_search"}, map[string]any{
			"kind":    "skills_search",
			"title":   "Skills matched",
			"summary": "5 skills",
		}, "5 skills"), nil
	})
	result, err := handler(context.Background(), toolRequest("skills_search", map[string]any{"query": "debug widget"}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("skills_search failed: result=%+v err=%v", result, err)
	}

	previous := mustActivitySnapshot(t, store, previousID, "")
	if previous.Active || len(previous.Events) != 1 || previous.Events[0].Path != "old.go" {
		t.Fatalf("previous prompt monitor was not archived intact: %+v", previous)
	}
	if result.Meta == nil || result.Meta.AdditionalFields == nil {
		t.Fatalf("skills_search result has no widget bootstrap metadata: %+v", result.Meta)
	}
	bootstrap := decodeStructuredResult[activityBootstrapView](t, result.Meta.AdditionalFields[activityBootstrapMeta])
	metadata := decodeStructuredResult[activityBootstrapView](t, result.Meta.AdditionalFields)
	if bootstrap.MonitorID == "" || bootstrap.MonitorID == previousID || metadata.MonitorID != bootstrap.MonitorID || metadata.Kind != "activity_bootstrap" {
		t.Fatalf("skills_search did not expose a new prompt monitor bootstrap: nested=%+v metadata=%+v", bootstrap, metadata)
	}
	current := mustActivitySnapshot(t, store, bootstrap.MonitorID, "")
	if !current.Active || len(current.Events) != 1 || current.Events[0].Tool != "skills_search" {
		t.Fatalf("skills_search was not recorded in the new prompt monitor: %+v", current)
	}
}

func TestActivityStoreIsolatesPromptMonitors(t *testing.T) {
	store := newActivityStore()
	firstID := store.startMonitor("ws_alpha")
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
	}, "alpha", false)

	secondID := store.startMonitor("ws_beta")
	store.record("write", map[string]any{
		"kind":        "write",
		"workspaceId": "ws_beta",
		"path":        "beta.go",
		"summary":     "beta updated",
	}, "beta", false)

	first := mustActivitySnapshot(t, store, firstID, "")
	second := mustActivitySnapshot(t, store, secondID, "")
	if first.Active || !second.Active {
		t.Fatalf("unexpected monitor activity state: first=%t second=%t", first.Active, second.Active)
	}
	if first.MonitorID != firstID || len(first.Events) != 2 || first.Events[0].Path != "alpha.go" {
		t.Fatalf("first prompt monitor changed or mixed events: %+v", first)
	}
	if second.MonitorID != secondID || len(second.Events) != 1 || second.Events[0].Path != "beta.go" {
		t.Fatalf("second prompt monitor is incomplete: %+v", second)
	}

	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_beta", "path": "later.go"}, "later", false)
	archived := mustActivitySnapshot(t, store, firstID, "")
	if archived.Sequence != first.Sequence || len(archived.Events) != len(first.Events) {
		t.Fatalf("archived prompt monitor received later activity: before=%+v after=%+v", first, archived)
	}
}

func TestActivityMiddlewareDropsLateCompletionAfterPromptSwitch(t *testing.T) {
	store := newActivityStore()
	firstID := store.startMonitor("ws_shared")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	handler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		close(started)
		<-release
		return newToolResult("read", map[string]any{"kind": "file"}, map[string]any{"kind": "file", "workspaceId": "ws_shared", "path": "shared.go"}, "late completion"), nil
	})

	go func() {
		_, err := handler(context.Background(), toolRequest("read", map[string]any{"workspace_id": "ws_shared"}))
		done <- err
	}()
	<-started
	secondID := store.startMonitor("ws_shared")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	first := mustActivitySnapshot(t, store, firstID, "")
	second := mustActivitySnapshot(t, store, secondID, "")
	if first.Active || len(first.Events) != 0 {
		t.Fatalf("archived monitor changed after the prompt switch: %+v", first)
	}
	if !second.Active || len(second.Events) != 0 {
		t.Fatalf("late activity from the old prompt leaked into the new monitor: %+v", second)
	}

	liveHandler := activityMiddleware(store)(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return newToolResult("read", map[string]any{"kind": "file"}, map[string]any{"kind": "file", "workspaceId": "ws_shared", "path": "shared.go"}, "late completion"), nil
	})
	if _, err := liveHandler(context.Background(), toolRequest("read", map[string]any{"workspace_id": "ws_shared"})); err != nil {
		t.Fatal(err)
	}
	second = mustActivitySnapshot(t, store, secondID, "")
	if len(second.Events) != 1 || second.Events[0].Summary != "late completion" || second.Events[0].Path != "shared.go" {
		t.Fatalf("new prompt activity was not recorded in its active monitor: %+v", second)
	}
}

func TestActivityStoreCanInspectOlderEvent(t *testing.T) {
	store := newActivityStore()
	monitorID := store.startMonitor("ws_one")
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_one", "path": "one.go", "content": "one"}, "one", false)
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_one", "path": "two.go", "content": "two"}, "two", false)
	latest := mustActivitySnapshot(t, store, monitorID, "")
	olderID := latest.Events[1].ID
	selected := mustActivitySnapshot(t, store, monitorID, olderID)
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

func TestActivityStoreKeepsOnlyRecentEventsPerMonitor(t *testing.T) {
	store := newActivityStore()
	monitorID := store.startMonitor("ws_limit")
	for index := 0; index < activityLimit+12; index++ {
		store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_limit", "path": index}, "read", false)
	}
	store.mu.RLock()
	monitor := store.monitors[monitorID]
	count := len(monitor.Events)
	firstSequence := monitor.Events[0].Sequence
	lastSequence := monitor.Events[len(monitor.Events)-1].Sequence
	store.mu.RUnlock()
	if count != activityLimit || firstSequence != 13 || lastSequence != activityLimit+12 {
		t.Fatalf("ring buffer count=%d first=%d last=%d", count, firstSequence, lastSequence)
	}
}

func TestActivitySnapshotJSONCarriesOneDetailedPayload(t *testing.T) {
	store := newActivityStore()
	monitorID := store.startMonitor("ws_json")
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_json", "path": "one.go", "content": "one"}, "one", false)
	store.record("read", map[string]any{"kind": "file", "workspaceId": "ws_json", "path": "two.go", "content": "two"}, "two", false)
	encoded, err := json.Marshal(mustActivitySnapshot(t, store, monitorID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(encoded, []byte(`"payload"`)) != 1 {
		t.Fatalf("snapshot should contain one detailed payload: %s", encoded)
	}
}
