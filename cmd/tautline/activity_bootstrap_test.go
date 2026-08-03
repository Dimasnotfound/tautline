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

func TestTautlineActivityMountsWithoutWorkspace(t *testing.T) {
	isolateWorkspaceRegistry(t, nil)
	result, err := handleTautlineActivity(context.Background(), toolRequest("tautline_activity", map[string]any{}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("tautline_activity failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[activityBootstrapView](t, result.StructuredContent)
	if view.Ready || view.WorkspaceID != "" || view.Kind != "activity_bootstrap" {
		t.Fatalf("unexpected empty bootstrap: %+v", view)
	}
}

func TestTautlineActivityRestoresActiveWorkspace(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{root})
	state := registerWorkspace(root)
	result, err := handleTautlineActivity(context.Background(), toolRequest("tautline_activity", map[string]any{}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("tautline_activity failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[activityBootstrapView](t, result.StructuredContent)
	if !view.Ready || view.WorkspaceID != state.ID || view.Path != root {
		t.Fatalf("active workspace was not restored: %+v", view)
	}
}

func TestActivitySnapshotUsesActiveWorkspaceWhenIDIsOmitted(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{root})
	state := registerWorkspace(root)
	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })
	app.activity.record("read", map[string]any{
		"kind":        "file",
		"workspaceId": state.ID,
		"path":        "README.md",
		"content":     "hello",
	}, "read", false)

	result, err := handleActivitySnapshot(context.Background(), toolRequest("activity_snapshot", map[string]any{}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("activity_snapshot failed: result=%+v err=%v", result, err)
	}
	snapshot := decodeStructuredResult[activitySnapshot](t, result.StructuredContent)
	if snapshot.WorkspaceID != state.ID || snapshot.WorkspacePath != root {
		t.Fatalf("snapshot did not use active workspace: %+v", snapshot)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Path != "README.md" {
		t.Fatalf("snapshot activity is incomplete: %+v", snapshot.Events)
	}
}
