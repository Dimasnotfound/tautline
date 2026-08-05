package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWorkspaceCreatesAndRestoresManagedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not available")
	}
	sourceRoot, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{sourceRoot})
	initializeTestRepository(t, sourceRoot)

	tracked := filepath.Join(sourceRoot, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("dirty checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })
	cfg := app.config.snapshot()
	worktreeRoot := effectiveWorktreeRoot(cfg)
	if err := configureWorkspacePersistence(cfg.RuntimeDir, worktreeRoot); err != nil {
		t.Fatal(err)
	}

	result, err := handleOpenWorkspace(context.Background(), toolRequest("open_workspace", map[string]any{
		"path": sourceRoot,
		"mode": workspaceModeWorktree,
	}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("open managed worktree failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[workspaceModelView](t, result.StructuredContent)
	if view.Mode != workspaceModeWorktree || view.Worktree == nil || !view.Worktree.Managed || !view.Worktree.Detached {
		t.Fatalf("managed worktree metadata is incomplete: %+v", view)
	}
	if view.SourceRoot != sourceRoot || view.Worktree.Path != view.Path {
		t.Fatalf("unexpected worktree paths: %+v", view)
	}
	if !view.Worktree.DirtySource {
		t.Fatal("dirty source checkout was not reported")
	}
	if !pathInsideRoot(worktreeRoot, view.Path) {
		t.Fatalf("worktree %q is outside configured root %q", view.Path, worktreeRoot)
	}
	worktreeContent, err := os.ReadFile(filepath.Join(view.Path, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if normalized := strings.ReplaceAll(string(worktreeContent), "\r\n", "\n"); normalized != "committed\n" {
		t.Fatalf("worktree copied uncommitted source changes: %q", worktreeContent)
	}
	if sourceContent, err := os.ReadFile(tracked); err != nil || strings.ReplaceAll(string(sourceContent), "\r\n", "\n") != "dirty checkout\n" {
		t.Fatalf("source checkout was modified: content=%q err=%v", sourceContent, err)
	}

	if sourceLookup, found := lookupWorkspaceByRoot(sourceRoot); found {
		t.Fatalf("worktree was incorrectly registered as the source checkout: %+v", sourceLookup)
	}
	opened, err := getWorkspace(view.WorkspaceID)
	if err != nil || opened.Mode != workspaceModeWorktree || opened.SourceRoot != sourceRoot {
		t.Fatalf("worktree workspace is not reusable: state=%+v err=%v", opened, err)
	}

	workspaceStore.mu.Lock()
	workspaceStore.byID = make(map[string]*workspaceState)
	workspaceStore.byRoot = make(map[string]*workspaceState)
	workspaceStore.activeID = ""
	workspaceStore.persistencePath = ""
	workspaceStore.managedWorktreeRoot = ""
	workspaceStore.mu.Unlock()
	if err := configureWorkspacePersistence(cfg.RuntimeDir, worktreeRoot); err != nil {
		t.Fatal(err)
	}
	restored, found := lookupWorkspaceByRoot(view.Path)
	if !found || restored.Mode != workspaceModeWorktree || restored.SourceRoot != sourceRoot || restored.Worktree == nil {
		t.Fatalf("managed worktree was not restored: state=%+v found=%v", restored, found)
	}
	lookupResult, err := handleWorkspaceLookup(context.Background(), toolRequest("workspace_lookup", map[string]any{"path": view.Path}))
	if err != nil || lookupResult == nil || lookupResult.IsError {
		t.Fatalf("restored worktree lookup failed: result=%+v err=%v", lookupResult, err)
	}
	lookup := decodeStructuredResult[workspaceLookupView](t, lookupResult.StructuredContent)
	if !lookup.Found || lookup.WorkspaceID != view.WorkspaceID {
		t.Fatalf("restored worktree lookup returned the wrong workspace: %+v", lookup)
	}

	t.Cleanup(func() {
		unregisterWorkspace(view.WorkspaceID)
		_ = removeManagedWorktree(context.Background(), sourceRoot, view.Path)
	})
}

func TestOpenWorkspaceRejectsInvalidWorktreeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not available")
	}
	sourceRoot, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{sourceRoot})
	initializeTestRepository(t, sourceRoot)
	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })

	result, err := handleOpenWorkspace(context.Background(), toolRequest("open_workspace", map[string]any{
		"path":     sourceRoot,
		"mode":     workspaceModeWorktree,
		"base_ref": "refs/heads/does-not-exist",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("invalid base ref was accepted: %+v", result)
	}
	if text := strings.ToLower(activityResultText(result)); !strings.Contains(text, "resolve worktree base") {
		t.Fatalf("unexpected invalid-base error: %s", text)
	}
}

func initializeTestRepository(t *testing.T, root string) {
	t.Helper()
	runGitTestCommand(t, root, "init", "-q")
	runGitTestCommand(t, root, "config", "user.name", "Tautline Test")
	runGitTestCommand(t, root, "config", "user.email", "tautline@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, root, "add", "tracked.txt")
	runGitTestCommand(t, root, "commit", "-q", "-m", "initial")
}

func runGitTestCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
