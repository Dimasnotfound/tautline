package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
	mu              sync.RWMutex
	byID            map[string]*workspaceState
	byRoot          map[string]*workspaceState
	activeID        string
	persistencePath string
}

type persistedWorkspaceRegistry struct {
	Roots             []string `json:"roots"`
	ActiveWorkspaceID string   `json:"activeWorkspaceId,omitempty"`
}

var workspaceStore = workspaceRegistry{
	byID:   make(map[string]*workspaceState),
	byRoot: make(map[string]*workspaceState),
}

func registerWorkspace(root string) *workspaceState {
	key := workspaceRootKey(root)
	workspaceStore.mu.Lock()
	if existing := workspaceStore.byRoot[key]; existing != nil {
		workspaceStore.mu.Unlock()
		return existing
	}
	state := registerWorkspaceLocked(root, key)
	shouldPersist := workspaceStore.persistencePath != ""
	workspaceStore.mu.Unlock()
	if shouldPersist {
		_ = persistWorkspaceRegistry()
	}
	return state
}

func registerWorkspaceLocked(root, key string) *workspaceState {
	hash := sha256.Sum256([]byte(key))
	state := &workspaceState{
		ID:        "ws_" + hex.EncodeToString(hash[:6]),
		Root:      root,
		originals: make(map[string]fileSnapshot),
	}
	workspaceStore.byRoot[key] = state
	workspaceStore.byID[state.ID] = state
	workspaceStore.activeID = state.ID
	return state
}

func configureWorkspacePersistence(runtimeDir string) error {
	path := filepath.Join(strings.TrimSpace(runtimeDir), "state", "workspaces.json")
	workspaceStore.mu.Lock()
	workspaceStore.persistencePath = path
	workspaceStore.mu.Unlock()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read workspace registry: %w", err)
	}
	var persisted persistedWorkspaceRegistry
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("decode workspace registry: %w", err)
	}

	validRoots := make([]string, 0, len(persisted.Roots))
	for _, root := range persisted.Roots {
		resolved, err := resolvePath(root)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}
		validRoots = append(validRoots, resolved)
	}

	workspaceStore.mu.Lock()
	for _, root := range validRoots {
		key := workspaceRootKey(root)
		if workspaceStore.byRoot[key] == nil {
			registerWorkspaceLocked(root, key)
		}
	}
	if persisted.ActiveWorkspaceID != "" && workspaceStore.byID[persisted.ActiveWorkspaceID] != nil {
		workspaceStore.activeID = persisted.ActiveWorkspaceID
	}
	workspaceStore.mu.Unlock()
	return persistWorkspaceRegistry()
}

func persistWorkspaceRegistry() error {
	workspaceStore.mu.RLock()
	path := workspaceStore.persistencePath
	activeID := workspaceStore.activeID
	roots := make([]string, 0, len(workspaceStore.byRoot))
	for _, state := range workspaceStore.byRoot {
		roots = append(roots, state.Root)
	}
	workspaceStore.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	sort.Strings(roots)
	data, err := json.MarshalIndent(persistedWorkspaceRegistry{Roots: roots, ActiveWorkspaceID: activeID}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return replaceWorkspaceStateFile(path, data)
}

func replaceWorkspaceStateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".workspaces-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace workspace registry: %w", err)
	}
	return nil
}

func lookupWorkspaceByRoot(root string) (*workspaceState, bool) {
	key := workspaceRootKey(root)
	workspaceStore.mu.RLock()
	state := workspaceStore.byRoot[key]
	workspaceStore.mu.RUnlock()
	return state, state != nil
}

func activateWorkspace(workspaceID string) (*workspaceState, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	workspaceStore.mu.Lock()
	state := workspaceStore.byID[workspaceID]
	if state != nil {
		workspaceStore.activeID = state.ID
	}
	shouldPersist := state != nil && workspaceStore.persistencePath != ""
	workspaceStore.mu.Unlock()
	if shouldPersist {
		_ = persistWorkspaceRegistry()
	}
	return state, state != nil
}

func defaultWorkspace() (*workspaceState, bool) {
	workspaceStore.mu.RLock()
	if state := workspaceStore.byID[workspaceStore.activeID]; state != nil {
		workspaceStore.mu.RUnlock()
		return state, true
	}
	states := make([]*workspaceState, 0, len(workspaceStore.byRoot))
	for _, state := range workspaceStore.byRoot {
		states = append(states, state)
	}
	workspaceStore.mu.RUnlock()
	if len(states) == 0 {
		return nil, false
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Root < states[j].Root })
	return states[0], true
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
