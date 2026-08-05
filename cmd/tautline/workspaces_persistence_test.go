package main

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateWorkspaceRegistry(t *testing.T, roots []string) {
	t.Helper()
	previousRoots := append([]string(nil), allowedRoots...)
	workspaceStore.mu.Lock()
	previousByID := workspaceStore.byID
	previousByRoot := workspaceStore.byRoot
	previousActiveID := workspaceStore.activeID
	previousPersistencePath := workspaceStore.persistencePath
	previousManagedWorktreeRoot := workspaceStore.managedWorktreeRoot
	workspaceStore.byID = make(map[string]*workspaceState)
	workspaceStore.byRoot = make(map[string]*workspaceState)
	workspaceStore.activeID = ""
	workspaceStore.persistencePath = ""
	workspaceStore.managedWorktreeRoot = ""
	workspaceStore.mu.Unlock()
	allowedRoots = append([]string(nil), roots...)
	t.Cleanup(func() {
		allowedRoots = previousRoots
		workspaceStore.mu.Lock()
		workspaceStore.byID = previousByID
		workspaceStore.byRoot = previousByRoot
		workspaceStore.activeID = previousActiveID
		workspaceStore.persistencePath = previousPersistencePath
		workspaceStore.managedWorktreeRoot = previousManagedWorktreeRoot
		workspaceStore.mu.Unlock()
	})
}

func TestWorkspaceRegistryPersistsAcrossRestart(t *testing.T) {
	projectRoot, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	isolateWorkspaceRegistry(t, []string{projectRoot})

	if err := configureWorkspacePersistence(runtimeDir); err != nil {
		t.Fatal(err)
	}
	opened := registerWorkspace(projectRoot)
	statePath := filepath.Join(runtimeDir, "state", "workspaces.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("workspace registry state was not written: %v", err)
	}

	workspaceStore.mu.Lock()
	workspaceStore.byID = make(map[string]*workspaceState)
	workspaceStore.byRoot = make(map[string]*workspaceState)
	workspaceStore.activeID = ""
	workspaceStore.persistencePath = ""
	workspaceStore.mu.Unlock()

	if err := configureWorkspacePersistence(runtimeDir); err != nil {
		t.Fatal(err)
	}
	restored, ok := lookupWorkspaceByRoot(projectRoot)
	if !ok {
		t.Fatal("workspace was not restored after simulated restart")
	}
	if restored.ID != opened.ID {
		t.Fatalf("restored workspace ID=%q, want %q", restored.ID, opened.ID)
	}
	if byID, err := getWorkspace(opened.ID); err != nil || byID.Root != projectRoot {
		t.Fatalf("restored workspace ID is not reusable: state=%+v err=%v", byID, err)
	}
	active, ok := defaultWorkspace()
	if !ok || active.ID != opened.ID {
		t.Fatalf("active workspace was not restored: state=%+v ok=%v", active, ok)
	}
}

func TestWorkspacePersistenceRestoresMostRecentActiveWorkspace(t *testing.T) {
	rootA, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	isolateWorkspaceRegistry(t, []string{rootA, rootB})
	if err := configureWorkspacePersistence(runtimeDir); err != nil {
		t.Fatal(err)
	}
	first := registerWorkspace(rootA)
	second := registerWorkspace(rootB)
	if _, ok := activateWorkspace(first.ID); !ok {
		t.Fatal("could not activate the first workspace")
	}

	workspaceStore.mu.Lock()
	workspaceStore.byID = make(map[string]*workspaceState)
	workspaceStore.byRoot = make(map[string]*workspaceState)
	workspaceStore.activeID = ""
	workspaceStore.persistencePath = ""
	workspaceStore.mu.Unlock()
	if err := configureWorkspacePersistence(runtimeDir); err != nil {
		t.Fatal(err)
	}
	active, ok := defaultWorkspace()
	if !ok || active.ID != first.ID || active.ID == second.ID {
		t.Fatalf("wrong active workspace restored: state=%+v ok=%v", active, ok)
	}
}

func TestWorkspacePersistenceIgnoresMissingOrDisallowedRoots(t *testing.T) {
	allowedRoot, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	stateDir := filepath.Join(runtimeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := `{"roots":["` + filepath.ToSlash(filepath.Join(allowedRoot, "missing")) + `","C:/outside-tautline"],"activeWorkspaceId":"ws_invalid"}`
	if err := os.WriteFile(filepath.Join(stateDir, "workspaces.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{allowedRoot})

	if err := configureWorkspacePersistence(runtimeDir); err != nil {
		t.Fatal(err)
	}
	workspaceStore.mu.RLock()
	count := len(workspaceStore.byID)
	activeID := workspaceStore.activeID
	workspaceStore.mu.RUnlock()
	if count != 0 || activeID != "" {
		t.Fatalf("invalid persisted workspace state was restored: count=%d active=%q", count, activeID)
	}
}
