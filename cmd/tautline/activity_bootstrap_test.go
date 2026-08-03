package main

import (
	"context"
	"encoding/json"
	"testing"
)

func decodeStructuredResult[T any](t *testing.T, value any) T {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded T
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func installTestRuntime(t *testing.T) *applicationRuntime {
	t.Helper()
	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })
	return app
}

func TestTautlineActivityMountsPromptMonitorWithoutWorkspace(t *testing.T) {
	isolateWorkspaceRegistry(t, nil)
	installTestRuntime(t)
	result, err := handleTautlineActivity(context.Background(), toolRequest("tautline_activity", map[string]any{}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("tautline_activity failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[activityBootstrapView](t, result.StructuredContent)
	if view.Ready || view.WorkspaceID != "" || view.Kind != "activity_bootstrap" || view.MonitorID == "" {
		t.Fatalf("unexpected empty bootstrap: %+v", view)
	}
}

func TestTautlineActivityRestoresWorkspaceAndCreatesUniquePromptMonitor(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{root})
	state := registerWorkspace(root)
	installTestRuntime(t)

	firstResult, err := handleTautlineActivity(context.Background(), toolRequest("tautline_activity", map[string]any{}))
	if err != nil || firstResult == nil || firstResult.IsError {
		t.Fatalf("first tautline_activity failed: result=%+v err=%v", firstResult, err)
	}
	secondResult, err := handleTautlineActivity(context.Background(), toolRequest("tautline_activity", map[string]any{}))
	if err != nil || secondResult == nil || secondResult.IsError {
		t.Fatalf("second tautline_activity failed: result=%+v err=%v", secondResult, err)
	}
	first := decodeStructuredResult[activityBootstrapView](t, firstResult.StructuredContent)
	second := decodeStructuredResult[activityBootstrapView](t, secondResult.StructuredContent)
	if !first.Ready || first.WorkspaceID != state.ID || first.Path != root || first.MonitorID == "" {
		t.Fatalf("active workspace was not restored: %+v", first)
	}
	if second.MonitorID == "" || second.MonitorID == first.MonitorID {
		t.Fatalf("prompt monitors are not unique: first=%+v second=%+v", first, second)
	}
}

func TestActivitySnapshotRequiresAndUsesPromptMonitor(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{root})
	state := registerWorkspace(root)
	app := installTestRuntime(t)

	bootstrapResult, err := handleTautlineActivity(context.Background(), toolRequest("tautline_activity", map[string]any{}))
	if err != nil || bootstrapResult == nil || bootstrapResult.IsError {
		t.Fatalf("tautline_activity failed: result=%+v err=%v", bootstrapResult, err)
	}
	bootstrap := decodeStructuredResult[activityBootstrapView](t, bootstrapResult.StructuredContent)
	app.activity.record("read", map[string]any{
		"kind":        "file",
		"workspaceId": state.ID,
		"path":        "README.md",
		"content":     "hello",
	}, "read", false)

	missingResult, err := handleActivitySnapshot(context.Background(), toolRequest("activity_snapshot", map[string]any{}))
	if err != nil || missingResult == nil || !missingResult.IsError {
		t.Fatalf("activity_snapshot accepted a missing monitor_id: result=%+v err=%v", missingResult, err)
	}
	result, err := handleActivitySnapshot(context.Background(), toolRequest("activity_snapshot", map[string]any{"monitor_id": bootstrap.MonitorID}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("activity_snapshot failed: result=%+v err=%v", result, err)
	}
	snapshot := decodeStructuredResult[activitySnapshot](t, result.StructuredContent)
	if snapshot.MonitorID != bootstrap.MonitorID || snapshot.WorkspaceID != state.ID || snapshot.WorkspacePath != root || !snapshot.Active {
		t.Fatalf("snapshot did not use the prompt monitor: %+v", snapshot)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Path != "README.md" {
		t.Fatalf("snapshot activity is incomplete: %+v", snapshot.Events)
	}
}
