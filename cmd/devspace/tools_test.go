package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnifiedDiffOnlyShowsInsertedLine(t *testing.T) {
	before := "alpha\nbeta\n"
	after := "alpha\ninserted\nbeta\n"
	diff, added, removed, truncated := unifiedDiff("README.md", before, after)
	if truncated {
		t.Fatal("small diff must not be truncated")
	}
	if added != 1 || removed != 0 {
		t.Fatalf("unexpected stats: added=%d removed=%d", added, removed)
	}
	if !strings.Contains(diff, "+inserted") {
		t.Fatalf("inserted line missing from diff:\n%s", diff)
	}
	if strings.Contains(diff, "-beta") || strings.Contains(diff, "+beta") {
		t.Fatalf("unchanged line was incorrectly marked as changed:\n%s", diff)
	}
}

func TestAtomicWriteFileReplacesExistingFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "sample.txt")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("after"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after" {
		t.Fatalf("unexpected file content: %q", data)
	}
}

func TestResolvePathRejectsSiblingPrefix(t *testing.T) {
	root := t.TempDir()
	allowedRoots = []string{root}
	inside := filepath.Join(root, "project", "main.go")
	if _, err := resolvePath(inside); err != nil {
		t.Fatalf("inside path rejected: %v", err)
	}
	sibling := root + "-other"
	if _, err := resolvePath(filepath.Join(sibling, "main.go")); err == nil {
		t.Fatal("sibling path with matching prefix must be rejected")
	}
}

func TestSignedAccessToken(t *testing.T) {
	previous := ownerToken
	ownerToken = "test-secret"
	t.Cleanup(func() { ownerToken = previous })

	token := issueSignedToken("access", "devspace", time.Minute)
	claims, valid := validateSignedToken(token, "access")
	if !valid {
		t.Fatal("fresh access token was rejected")
	}
	if claims.Scope != "devspace" {
		t.Fatalf("unexpected scope: %s", claims.Scope)
	}
	if _, valid := validateSignedToken(token+"tampered", "access"); valid {
		t.Fatal("tampered token was accepted")
	}
	if _, valid := validateSignedToken(token, "refresh"); valid {
		t.Fatal("access token was accepted as refresh token")
	}
}

func TestLanguageFromPath(t *testing.T) {
	tests := map[string]string{
		"main.go":       "go",
		"app.tsx":       "typescript",
		"package.json":  "json",
		"README.md":     "markdown",
		"start-dev.cmd": "batch",
		"unknown.xyz":   "text",
	}
	for path, expected := range tests {
		if actual := languageFromPath(path); actual != expected {
			t.Fatalf("languageFromPath(%q)=%q, want %q", path, actual, expected)
		}
	}
}

func TestWidgetDocumentsAreLightweight(t *testing.T) {
	widgets := map[string]string{
		workspaceWidgetURI: workspaceWidgetHTML(),
		fileWidgetURI:      fileWidgetHTML(),
		diffWidgetURI:      diffWidgetHTML(),
		commandWidgetURI:   commandWidgetHTML(),
	}
	if len(widgets) != 4 {
		t.Fatalf("unexpected widget count: %d", len(widgets))
	}
	for uri, html := range widgets {
		if len(html) > 20*1024 {
			t.Fatalf("widget %s is too large: %d bytes", uri, len(html))
		}
		if strings.Contains(html, "<script src=") || strings.Contains(html, "https://") {
			t.Fatalf("widget %s must not load external assets", uri)
		}
		if !strings.Contains(html, "requestDisplayMode") {
			t.Fatalf("widget %s has no fullscreen support", uri)
		}
		if !strings.Contains(html, "ui/notifications/tool-result") {
			t.Fatalf("widget %s has no MCP Apps result listener", uri)
		}
		if !strings.Contains(html, "devspace/widgetData") {
			t.Fatalf("widget %s cannot read widget-only tool metadata", uri)
		}
		if strings.Contains(html, "max-height") {
			t.Fatalf("widget %s reintroduced nested vertical scrolling", uri)
		}
	}
}

func TestWorkspaceModelPayloadReduction(t *testing.T) {
	files := make([]viewFile, 350)
	for index := range files {
		files[index] = viewFile{
			Name: "item.go",
			Path: fmt.Sprintf("internal/module-%02d/item-%03d.go", index/25, index),
			Type: "file",
			Size: int64(1200 + index),
		}
	}
	stats := toolStats{Entries: 350, Directories: 50, Files: 300, Bytes: 412350}
	full := workspaceView{Kind: "workspace", Title: "sample", Path: "C:/sample", Files: files, Stats: stats}
	compact := workspaceModelView{Kind: full.Kind, Title: full.Title, Path: full.Path, Files: files[:modelWorkspaceEntries], Stats: stats, Truncated: true}

	fullJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	compactJSON, err := json.Marshal(compact)
	if err != nil {
		t.Fatal(err)
	}
	if float64(len(compactJSON))/float64(len(fullJSON)) >= 0.10 {
		t.Fatalf("compact workspace payload is not at least 90%% smaller: full=%d compact=%d", len(fullJSON), len(compactJSON))
	}

	result := newWidgetToolResult(compact, full, "workspace ready")
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "devspace/widgetData") {
		t.Fatal("full widget data was not attached to hidden tool metadata")
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	canonicalRoot, err := canonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	previous := allowedRoots
	allowedRoots = []string{canonicalRoot}
	t.Cleanup(func() { allowedRoots = previous })

	if _, err := resolvePath(filepath.Join(link, "secret.txt")); err == nil {
		t.Fatal("symlink escape outside an allowed root was accepted")
	}
}
