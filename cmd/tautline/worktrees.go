package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	workspaceModeCheckout = "checkout"
	workspaceModeWorktree = "worktree"
)

type workspaceWorktree struct {
	Path        string `json:"path"`
	BaseRef     string `json:"baseRef"`
	BaseSHA     string `json:"baseSha"`
	DirtySource bool   `json:"dirtySource"`
	Detached    bool   `json:"detached"`
	Managed     bool   `json:"managed"`
}

type managedWorktreeResult struct {
	SourceRoot string
	Worktree   workspaceWorktree
}

var worktreeNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func effectiveWorktreeRoot(cfg TautlineConfig) string {
	root := strings.TrimSpace(cfg.WorktreeRoot)
	if root == "" || root == "." {
		root = filepath.Join(cfg.RuntimeDir, "worktrees")
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return filepath.Clean(root)
}

func createManagedWorktree(ctx context.Context, sourcePath, baseRef, worktreeRoot string) (managedWorktreeResult, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return managedWorktreeResult{}, errors.New("managed worktrees require Git on PATH")
	}

	sourceRootOutput, err := runGitOutput(ctx, sourcePath, "rev-parse", "--show-toplevel")
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("resolve Git repository root: %w", err)
	}
	sourceRoot, err := canonicalPath(strings.TrimSpace(sourceRootOutput))
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("canonicalize Git repository root: %w", err)
	}
	if _, err := resolvePath(sourceRoot); err != nil {
		return managedWorktreeResult{}, fmt.Errorf("Git repository root is outside the allowed roots: %w", err)
	}

	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	baseSHAOutput, err := runGitOutput(ctx, sourceRoot, "rev-parse", "--verify", baseRef+"^{commit}")
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("resolve worktree base %q: %w", baseRef, err)
	}
	baseSHA := strings.TrimSpace(baseSHAOutput)
	if baseSHA == "" {
		return managedWorktreeResult{}, fmt.Errorf("worktree base %q did not resolve to a commit", baseRef)
	}

	statusOutput, err := runGitOutput(ctx, sourceRoot, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("inspect source checkout: %w", err)
	}

	root, err := filepath.Abs(strings.TrimSpace(worktreeRoot))
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("resolve managed worktree root: %w", err)
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return managedWorktreeResult{}, fmt.Errorf("create managed worktree root: %w", err)
	}
	canonicalRoot, err := canonicalPath(root)
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("canonicalize managed worktree root: %w", err)
	}

	name := worktreeNameSanitizer.ReplaceAllString(filepath.Base(sourceRoot), "-")
	name = strings.Trim(strings.TrimSpace(name), "-.")
	if name == "" {
		name = "workspace"
	}
	folder := fmt.Sprintf("%s-%s-%s", name, time.Now().UTC().Format("20060102-150405"), randomHex(4))
	destination := filepath.Join(canonicalRoot, folder)
	if !pathInsideRoot(canonicalRoot, destination) {
		return managedWorktreeResult{}, errors.New("managed worktree destination escaped its configured root")
	}

	if _, err := runGitOutput(ctx, sourceRoot, "worktree", "add", "--detach", destination, baseSHA); err != nil {
		_ = os.RemoveAll(destination)
		return managedWorktreeResult{}, fmt.Errorf("create managed Git worktree: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeManagedWorktree(context.Background(), sourceRoot, destination)
		}
	}()

	canonicalDestination, err := canonicalPath(destination)
	if err != nil {
		return managedWorktreeResult{}, fmt.Errorf("canonicalize managed worktree: %w", err)
	}
	if !pathInsideRoot(canonicalRoot, canonicalDestination) {
		return managedWorktreeResult{}, errors.New("managed worktree was created outside its configured root")
	}

	cleanup = false
	return managedWorktreeResult{
		SourceRoot: sourceRoot,
		Worktree: workspaceWorktree{
			Path:        canonicalDestination,
			BaseRef:     baseRef,
			BaseSHA:     baseSHA,
			DirtySource: strings.TrimSpace(statusOutput) != "",
			Detached:    true,
			Managed:     true,
		},
	}, nil
}

func removeManagedWorktree(ctx context.Context, sourceRoot, destination string) error {
	removeContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, gitErr := runGitOutput(removeContext, sourceRoot, "worktree", "remove", "--force", destination)
	removeErr := os.RemoveAll(destination)
	if gitErr != nil && removeErr != nil {
		return fmt.Errorf("remove worktree: %v; remove directory: %w", gitErr, removeErr)
	}
	if removeErr != nil {
		return removeErr
	}
	return nil
}

func runGitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return string(output), nil
}
