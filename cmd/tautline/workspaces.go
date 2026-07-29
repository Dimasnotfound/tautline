package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type fileSnapshot struct {
	Exists  bool
	Content string
}

type workspaceState struct {
	ID        string
	Root      string
	mu        sync.Mutex
	originals map[string]fileSnapshot
}

type workspaceRegistry struct {
	mu     sync.RWMutex
	byID   map[string]*workspaceState
	byRoot map[string]*workspaceState
}

var workspaceStore = workspaceRegistry{
	byID:   make(map[string]*workspaceState),
	byRoot: make(map[string]*workspaceState),
}

func registerWorkspace(root string) *workspaceState {
	key := workspaceRootKey(root)
	workspaceStore.mu.Lock()
	defer workspaceStore.mu.Unlock()
	if existing := workspaceStore.byRoot[key]; existing != nil {
		return existing
	}
	hash := sha256.Sum256([]byte(key))
	state := &workspaceState{
		ID:        "ws_" + hex.EncodeToString(hash[:6]),
		Root:      root,
		originals: make(map[string]fileSnapshot),
	}
	workspaceStore.byRoot[key] = state
	workspaceStore.byID[state.ID] = state
	return state
}

func workspaceRootKey(root string) string {
	clean := filepath.Clean(root)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func getWorkspace(workspaceID string) (*workspaceState, error) {
	workspaceStore.mu.RLock()
	state := workspaceStore.byID[strings.TrimSpace(workspaceID)]
	workspaceStore.mu.RUnlock()
	if state == nil {
		return nil, fmt.Errorf("unknown workspace_id %q; call open_workspace first", workspaceID)
	}
	return state, nil
}

func resolveWorkspacePath(workspaceID, relativePath string) (*workspaceState, string, error) {
	state, err := getWorkspace(workspaceID)
	if err != nil {
		return nil, "", err
	}
	path := strings.TrimSpace(relativePath)
	if path == "" {
		return nil, "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return nil, "", fmt.Errorf("path must be relative to workspace %s", workspaceID)
	}
	cleanRelative := filepath.Clean(path)
	if cleanRelative == ".." || strings.HasPrefix(cleanRelative, ".."+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("path escapes workspace %s", workspaceID)
	}
	candidate, err := canonicalPath(filepath.Join(state.Root, cleanRelative))
	if err != nil {
		return nil, "", err
	}
	if !pathInsideRoot(state.Root, candidate) {
		return nil, "", fmt.Errorf("path escapes workspace %s", workspaceID)
	}
	return state, candidate, nil
}

func resolveWorkspaceDirectory(workspaceID, relativePath string) (*workspaceState, string, error) {
	if strings.TrimSpace(relativePath) == "" || strings.TrimSpace(relativePath) == "." {
		state, err := getWorkspace(workspaceID)
		if err != nil {
			return nil, "", err
		}
		return state, state.Root, nil
	}
	state, path, err := resolveWorkspacePath(workspaceID, relativePath)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("not a directory: %s", relativePath)
	}
	return state, path, nil
}

func pathInsideRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative))
}

func (state *workspaceState) relativePath(absolutePath string) (string, error) {
	relative, err := filepath.Rel(state.Root, absolutePath)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path is outside workspace %s", state.ID)
	}
	return filepath.ToSlash(relative), nil
}

func (state *workspaceState) recordOriginal(absolutePath string, snapshot fileSnapshot) error {
	relative, err := state.relativePath(absolutePath)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if _, exists := state.originals[relative]; !exists {
		state.originals[relative] = snapshot
	}
	return nil
}
