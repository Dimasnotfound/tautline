package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
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

	token := issueSignedToken("access", "tautline", time.Minute)
	claims, valid := validateSignedToken(token, "access")
	if !valid {
		t.Fatal("fresh access token was rejected")
	}
	if claims.Scope != "tautline" {
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

func TestWidgetDocumentIsLightweight(t *testing.T) {
	html := toolCardWidgetHTML()
	if len(html) > 36*1024 {
		t.Fatalf("tool card widget is too large: %d bytes", len(html))
	}
	if strings.Contains(html, "<script src=") || strings.Contains(html, "https://") {
		t.Fatal("tool card widget must not load external assets")
	}
	for _, marker := range []string{
		"ui/notifications/tool-result",
		"devspace/widgetData",
		"Opened workspace",
		"Searched workspace",
		"Read file",
		"Wrote file",
		"Edited file",
		"Ran command",
		"Read artifact",
		"Changed ",
		"Matched Hermes skills",
		"Loaded skill",
		"Read skill file",
		"skill-badge",
		"tool-header",
		"tool-icon",
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("tool card widget is missing %q", marker)
		}
	}
}

func TestAllWidgetTemplatesAreSelfContainedAndSyntacticallyValid(t *testing.T) {
	templates := map[string]string{
		"tool-card": toolCardWidgetHTML(),
		"workspace": workspaceWidgetHTML(),
		"file":      fileWidgetHTML(),
		"diff":      diffWidgetHTML(),
		"command":   commandWidgetHTML(),
		"changes":   changesWidgetHTML(),
	}
	nodePath, nodeErr := exec.LookPath("node")
	for name, html := range templates {
		t.Run(name, func(t *testing.T) {
			lower := strings.ToLower(html)
			for _, marker := range []string{"<!doctype html>", "<html", "<head>", "<body", "<script>", "</script>", "</html>"} {
				if !strings.Contains(lower, marker) {
					t.Fatalf("template is missing %q", marker)
				}
			}
			if strings.Contains(lower, "<script src=") || strings.Contains(lower, "<link rel=\"stylesheet\"") || strings.Contains(lower, "https://") {
				t.Fatal("template must be self-contained and must not load external assets")
			}
			start := strings.Index(lower, "<script>")
			end := strings.LastIndex(lower, "</script>")
			if start < 0 || end <= start {
				t.Fatal("template script could not be extracted")
			}
			if nodeErr != nil {
				t.Log("node unavailable; skipped JavaScript syntax validation")
				return
			}
			script := html[start+len("<script>") : end]
			path := filepath.Join(t.TempDir(), name+".js")
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(nodePath, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("invalid JavaScript: %v\n%s", err, output)
			}
		})
	}
}

func TestActiveWidgetHasFailureAndResizeRecovery(t *testing.T) {
	html := toolCardWidgetHTML()
	for _, marker := range []string{
		"renderFailure",
		"Tautline template could not render",
		"ui/notifications/size-changed",
		"ResizeObserver",
		"envelope.isError",
		"typeof envelope.kind",
		"payload.status",
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("hardened widget is missing %q", marker)
		}
	}
}

func TestWidgetIntrinsicHeightUsesRenderedContentOnly(t *testing.T) {
	templates := map[string]string{
		"tool-card": toolCardWidgetHTML(),
		"shared":    widgetDocument("test", "mount(function() {});"),
	}
	for name, html := range templates {
		t.Run(name, func(t *testing.T) {
			for _, forbidden := range []string{
				"document.body.scrollHeight",
				"document.documentElement.scrollHeight",
				"window.innerHeight",
			} {
				if strings.Contains(html, forbidden) {
					t.Fatalf("template measures iframe viewport height through %q", forbidden)
				}
			}
			for _, required := range []string{
				"app.firstElementChild || app",
				"getBoundingClientRect()",
				"ui/notifications/size-changed",
			} {
				if !strings.Contains(html, required) {
					t.Fatalf("content-sized template is missing %q", required)
				}
			}
		})
	}
}

func TestWidgetResourceAliasesAreUniqueAndCompatible(t *testing.T) {
	expected := map[string]bool{
		"ui://tautline/tool-card-v2.html":      false,
		"ui://tautline/tool-card-v1.html":      false,
		"ui://devspace/tool-card-v5.html":      false,
		"ui://devspace/tool-card-v4.html":      false,
		"ui://devspace/tool-card-v3.html":      false,
		"ui://devspace/tool-card-v2.html":      false,
		"ui://devspace/tool-card-v1.html":      false,
		"ui://devspace/workspace-card-v3.html": false,
		"ui://devspace/workspace-card-v2.html": false,
		"ui://devspace/file-viewer-v2.html":    false,
		"ui://devspace/diff-viewer-v2.html":    false,
		"ui://devspace/command-result-v2.html": false,
		"ui://devspace/changes-review-v1.html": false,
	}
	definitions := widgetResourceDefinitions()
	if len(definitions) != len(expected) {
		t.Fatalf("unexpected widget resource count: got %d want %d", len(definitions), len(expected))
	}
	for _, definition := range definitions {
		seen, exists := expected[definition.URI]
		if !exists {
			t.Fatalf("unexpected widget URI %q", definition.URI)
		}
		if seen {
			t.Fatalf("duplicate widget URI %q", definition.URI)
		}
		expected[definition.URI] = true
	}
	for uri, seen := range expected {
		if !seen {
			t.Fatalf("missing compatibility URI %q", uri)
		}
	}
}

func TestWidgetDomainMetadataUsesDedicatedOrigin(t *testing.T) {
	t.Setenv("DEVSPACE_WIDGET_DOMAIN", "")
	t.Setenv("DEVSPACE_PUBLIC_BASE_URL", "https://mcp.example.test/mcp?ignored=true")

	meta := widgetResourceMetaMap("DevSpace test widget")
	uiMeta, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatal("ui metadata is missing or invalid")
	}
	const publicOrigin = "https://mcp.example.test"
	if uiMeta["domain"] != publicOrigin {
		t.Fatalf("ui.domain=%v, want %s", uiMeta["domain"], publicOrigin)
	}
	if meta["openai/widgetDomain"] != publicOrigin {
		t.Fatalf("openai/widgetDomain=%v, want %s", meta["openai/widgetDomain"], publicOrigin)
	}

	t.Setenv("DEVSPACE_WIDGET_DOMAIN", "https://widgets.example.test/app/path")
	meta = widgetResourceMetaMap("DevSpace test widget")
	uiMeta = meta["ui"].(map[string]any)
	const dedicatedOrigin = "https://widgets.example.test"
	if uiMeta["domain"] != dedicatedOrigin || meta["openai/widgetDomain"] != dedicatedOrigin {
		t.Fatalf("explicit widget domain did not override public base URL: %+v", meta)
	}
}

func TestWidgetDomainRejectsInsecureOrigin(t *testing.T) {
	t.Setenv("DEVSPACE_WIDGET_DOMAIN", "http://widgets.example.test")
	t.Setenv("DEVSPACE_PUBLIC_BASE_URL", "")
	meta := widgetResourceMetaMap("DevSpace test widget")
	uiMeta := meta["ui"].(map[string]any)
	if _, exists := uiMeta["domain"]; exists {
		t.Fatal("insecure ui.domain was accepted")
	}
	if _, exists := meta["openai/widgetDomain"]; exists {
		t.Fatal("insecure openai/widgetDomain was accepted")
	}
}

func TestSkillRankingPrefersExactAndFiltersIncompatible(t *testing.T) {
	skills := []skillMetadata{
		{
			Name:        "humanizer",
			Identifier:  "creative/humanizer",
			Category:    "creative",
			Description: "Humanize text and remove AI writing patterns.",
			Tags:        []string{"writing", "editing"},
			Compatible:  true,
		},
		{
			Name:        "technical-writing",
			Identifier:  "communication/technical-writing",
			Category:    "communication",
			Description: "Write technical documentation.",
			Tags:        []string{"writing"},
			Compatible:  true,
		},
		{
			Name:                 "macos-computer-use",
			Identifier:           "apple/macos-computer-use",
			Category:             "apple",
			Description:          "Control macOS desktop applications.",
			Compatible:           false,
			CompatibilityReasons: []string{"platform is not compatible"},
		},
	}

	matches := rankSkills("humanizer", skills, false)
	if len(matches) == 0 || matches[0].Name != "humanizer" {
		t.Fatalf("exact skill match did not rank first: %+v", matches)
	}
	for _, match := range rankSkills("macos computer use", skills, false) {
		if match.Name == "macos-computer-use" {
			t.Fatal("incompatible skill was returned without include_incompatible")
		}
	}
	matches = rankSkills("macos computer use", skills, true)
	if len(matches) == 0 || matches[0].Name != "macos-computer-use" {
		t.Fatalf("explicit incompatible search did not return the expected skill: %+v", matches)
	}
}

func TestSensitiveTextRedaction(t *testing.T) {
	input := "api_key=sk-test-1234567890\nAuthorization: Bearer abcdefghijklmnop\nhttps://example.test/?token=secret-value"
	redacted, changed := redactSensitiveText(input)
	if !changed {
		t.Fatal("secret-looking input was not marked as redacted")
	}
	for _, secret := range []string{"sk-test-1234567890", "abcdefghijklmnop", "secret-value"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q remained in redacted output: %s", secret, redacted)
		}
	}
	if strings.Count(redacted, "[REDACTED]") != 3 {
		t.Fatalf("unexpected redaction output: %s", redacted)
	}
}

func TestSkillCacheKeyTracksSnapshot(t *testing.T) {
	directory := t.TempDir()
	snapshot := filepath.Join(directory, "snapshot.json")
	if err := os.WriteFile(snapshot, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := skillBridgeConfig{
		Home:           directory,
		AgentDir:       filepath.Join(directory, "agent"),
		Python:         filepath.Join(directory, "python"),
		Snapshot:       snapshot,
		AvailableTools: []string{"read", "write"},
		Toolsets:       []string{"files"},
	}
	first := skillCacheKey(config)
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(snapshot, []byte("two-two"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := skillCacheKey(config)
	if first == second {
		t.Fatal("snapshot modification did not invalidate the skill cache key")
	}
}

func TestWorkflowInstructionsRequireSkillMatching(t *testing.T) {
	instructions := codingWorkflowInstructions()
	for _, expected := range []string{"skills_search", "skill_view", "Before every non-trivial task", "never expose secret configuration values"} {
		if !strings.Contains(instructions, expected) {
			t.Fatalf("workflow instructions are missing %q", expected)
		}
	}
}

func TestInstalledHermesSkillsIntegration(t *testing.T) {
	if os.Getenv("DEVSPACE_TEST_HERMES_SKILLS") != "1" {
		t.Skip("set DEVSPACE_TEST_HERMES_SKILLS=1 to run the installed-skill integration test")
	}
	expected := 0
	if raw := strings.TrimSpace(os.Getenv("DEVSPACE_EXPECTED_HERMES_SKILLS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("invalid DEVSPACE_EXPECTED_HERMES_SKILLS: %v", err)
		}
		expected = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var result struct {
		Success             bool `json:"success"`
		Installed           int  `json:"installed"`
		Resolved            int  `json:"resolved"`
		Viewed              int  `json:"viewed"`
		ExpectedUnsupported int  `json:"expected_unsupported"`
		SupportFilesRead    int  `json:"support_files_read"`
		ReadOnlyVerified    bool `json:"read_only_verified"`
		Failures            []struct {
			Skill string `json:"skill"`
			Error string `json:"error"`
		} `json:"failures"`
	}
	if err := runSkillBridge(ctx, bridgeRequest{Action: "self_test", FullView: true}, &result); err != nil {
		t.Fatal(err)
	}
	if expected > 0 && result.Installed != expected {
		t.Fatalf("installed skill count=%d, want %d", result.Installed, expected)
	}
	if !result.Success || !result.ReadOnlyVerified || result.Resolved != result.Installed {
		t.Fatalf("Hermes skill integration failed: installed=%d resolved=%d viewed=%d unsupported=%d support_files=%d read_only=%v failures=%+v",
			result.Installed, result.Resolved, result.Viewed, result.ExpectedUnsupported, result.SupportFilesRead, result.ReadOnlyVerified, result.Failures)
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

func TestParseWidgetMode(t *testing.T) {
	for input, expected := range map[string]widgetMode{"": widgetModeFull, "changes": widgetModeChanges, "full": widgetModeFull, "off": widgetModeOff} {
		actual, err := parseWidgetMode(input)
		if err != nil || actual != expected {
			t.Fatalf("parseWidgetMode(%q)=%q, %v; want %q", input, actual, err, expected)
		}
	}
	if _, err := parseWidgetMode("busy"); err == nil {
		t.Fatal("invalid widget mode was accepted")
	}
}

func TestChangesModeKeepsSubagentWidgets(t *testing.T) {
	previousMode := activeWidgetMode
	activeWidgetMode = widgetModeChanges
	t.Cleanup(func() { activeWidgetMode = previousMode })

	for _, toolName := range []string{"open_workspace", "show_changes", "list_subagents", "delegate_task", "get_agent_run", "cancel_agent_run"} {
		if !widgetEnabledForTool(toolName) {
			t.Fatalf("changes mode must enable the widget for %s", toolName)
		}
	}
	for _, toolName := range []string{"search", "read", "write", "edit", "bash", "skills_search", "lightpanda_fetch"} {
		if widgetEnabledForTool(toolName) {
			t.Fatalf("changes mode unexpectedly enabled the widget for %s", toolName)
		}
	}

	tool := mcp.NewTool("delegate_task")
	maybeSetWidgetMeta("delegate_task", &tool, toolCardWidgetURI, "Delegating task", "Task delegated")
	encodedTool, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"openai/outputTemplate", "ui://tautline/tool-card-v2.html", "resourceUri"} {
		if !strings.Contains(string(encodedTool), marker) {
			t.Fatalf("delegate_task metadata is missing %q: %s", marker, encodedTool)
		}
	}

	result := newToolResult("delegate_task", map[string]string{"kind": "agent_run"}, map[string]string{"kind": "agent_run"}, "delegated")
	encodedResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encodedResult), "tautline/widgetData") {
		t.Fatal("delegate_task result is missing widget data in changes mode")
	}
}

func TestWorkspaceRelativePathAndCheckpoint(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := registerWorkspace(root)
	path := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.recordOriginal(path, fileSnapshot{Exists: true, Content: "before\n"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model, widget, err := state.buildChangeReview()
	if err != nil {
		t.Fatal(err)
	}
	if model.Stats.Files != 1 || model.Stats.Added != 1 || model.Stats.Removed != 1 {
		t.Fatalf("unexpected review stats: %+v", model.Stats)
	}
	if !strings.Contains(widget.Diff, "+after") || !strings.Contains(widget.Diff, "-before") {
		t.Fatalf("aggregate diff missing content:\n%s", widget.Diff)
	}
	second, _, err := state.buildChangeReview()
	if err != nil {
		t.Fatal(err)
	}
	if !second.Empty {
		t.Fatal("show_changes did not advance the review checkpoint")
	}
	if _, _, err := resolveWorkspacePath(state.ID, "../escape.txt"); err == nil {
		t.Fatal("workspace-relative path escape was accepted")
	}
}

func TestReadLineWindowAndFreshness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	window, err := readLineWindow(path, 2, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if window.StartLine != 2 || window.EndLine != 3 || window.TotalLines != 5 {
		t.Fatalf("unexpected window metadata: %+v", window)
	}
	if !strings.Contains(window.Content, "2  two") || !strings.Contains(window.Content, "3  three") {
		t.Fatalf("window content is incorrect:\n%s", window.Content)
	}
	if !strings.HasPrefix(window.NextCursor, "line:4:") {
		t.Fatalf("unexpected next cursor: %q", window.NextCursor)
	}
	line, prefix, err := parseLineCursor(window.NextCursor)
	if err != nil || line != 4 || !hashMatchesExpected(window.Provenance.SHA256, prefix) {
		t.Fatalf("cursor did not preserve freshness: line=%d prefix=%q err=%v", line, prefix, err)
	}
	if err := os.WriteFile(path, []byte("one\ntwo changed\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := provenanceForFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hashMatchesExpected(updated.SHA256, window.Provenance.SHA256) {
		t.Fatal("modified file retained the previous hash")
	}
}

func TestWorkspaceReadRejectsStaleHash(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := registerWorkspace(root)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc one() {}\nfunc two() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := readWorkspaceFileWindow(toolRequest("read", map[string]any{
		"workspace_id": state.ID,
		"path":         "main.go",
		"start_line":   3,
		"max_lines":    1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first.StartLine != 3 || first.EndLine != 3 || first.SHA256 == "" {
		t.Fatalf("unexpected first read: %+v", first)
	}
	if err := os.WriteFile(path, []byte("package main\n\nfunc changed() {}\nfunc two() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = readWorkspaceFileWindow(toolRequest("read", map[string]any{
		"workspace_id":    state.ID,
		"path":            "main.go",
		"start_line":      3,
		"max_lines":       1,
		"expected_sha256": first.SHA256,
	}))
	if err == nil || !strings.Contains(err.Error(), "file changed") {
		t.Fatalf("stale read was not rejected: %v", err)
	}
}

func TestWorkspaceSearchIsBoundedAndExact(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := registerWorkspace(root)
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "one.go"), []byte("package internal\n\nfunc Alpha() {\n\tneedle := true\n\t_ = needle\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two.go"), []byte("package main\nvar Needle = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := handleWorkspaceSearch(context.Background(), toolRequest("search", map[string]any{
		"workspace_id":  state.ID,
		"query":         "needle",
		"glob":          "*.go,internal/*.go",
		"context_lines": 1,
		"max_results":   2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("search returned an error: %+v", result)
	}
	view, ok := result.StructuredContent.(searchView)
	if !ok {
		t.Fatalf("unexpected structured search type: %T", result.StructuredContent)
	}
	if len(view.Matches) != 2 || !view.Truncated {
		t.Fatalf("search was not bounded as expected: %+v", view)
	}
	for _, match := range view.Matches {
		if match.Line < 1 || match.SHA256 == "" || !strings.Contains(strings.ToLower(match.Excerpt), "needle") {
			t.Fatalf("search match lacks exact evidence: %+v", match)
		}
	}
}

func TestLargeCommandCaptureKeepsMiddleErrorAndRedacts(t *testing.T) {
	t.Setenv("DEVSPACE_ARTIFACT_DIR", t.TempDir())
	t.Setenv("DEVSPACE_INLINE_COMMAND_BYTES", "4096")
	root, err := ensureArtifactStore()
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := registerWorkspace(workspaceRoot)

	var builder strings.Builder
	for line := 1; line <= 320; line++ {
		if line == 170 {
			builder.WriteString("ERROR request failed api_key=supersecretvalue123456\n")
		} else {
			fmt.Fprintf(&builder, "normal output line %03d with enough repeated content to exceed the inline threshold\n", line)
		}
	}
	rawContent := builder.String()
	rawPath := filepath.Join(root, "tmp", "raw-test.log")
	if err := os.WriteFile(rawPath, []byte(rawContent), 0o600); err != nil {
		t.Fatal(err)
	}
	capture, err := finalizeCommandCapture(root, rawPath, state.ID, "go test ./... --api_key=commandsecret123456", int64(len(rawContent)), false)
	if err != nil {
		t.Fatal(err)
	}
	if capture.Artifact == nil || capture.OmittedLines <= 0 {
		t.Fatalf("large output did not create a compact artifact result: %+v", capture)
	}
	if !strings.Contains(capture.Output, "ERROR request failed") || !strings.Contains(capture.Output, "[REDACTED]") {
		t.Fatalf("middle error window or redaction is missing:\n%s", capture.Output)
	}
	if strings.Contains(capture.Output, "supersecretvalue123456") {
		t.Fatal("secret remained in the inline preview")
	}
	manifest, err := readArtifactManifest(root, capture.Artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(manifest.SourceLabel, "commandsecret123456") || !strings.Contains(manifest.SourceLabel, "[REDACTED]") || !manifest.Redacted {
		t.Fatalf("artifact manifest command label was not redacted: %+v", manifest)
	}
	blobPath, err := artifactBlobPath(root, manifest.BlobSHA256)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "supersecretvalue123456") || !strings.Contains(string(blob), "[REDACTED]") {
		t.Fatal("persisted artifact was not redacted before storage")
	}

	readResult, err := handleArtifactRead(context.Background(), toolRequest("artifact_read", map[string]any{
		"workspace_id": state.ID,
		"artifact_id":  capture.Artifact.ID,
		"start_line":   168,
		"end_line":     172,
	}))
	if err != nil || readResult.IsError {
		t.Fatalf("artifact read failed: err=%v result=%+v", err, readResult)
	}
	artifact, ok := readResult.StructuredContent.(artifactView)
	if !ok || !strings.Contains(artifact.Content, "ERROR request failed") {
		t.Fatalf("artifact range did not return exact evidence: %T %+v", readResult.StructuredContent, readResult.StructuredContent)
	}

	otherRoot, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other := registerWorkspace(otherRoot)
	crossResult, err := handleArtifactRead(context.Background(), toolRequest("artifact_read", map[string]any{
		"workspace_id": other.ID,
		"artifact_id":  capture.Artifact.ID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !crossResult.IsError {
		t.Fatal("artifact was readable from a different workspace")
	}
}

func TestSmallCommandCaptureStaysInline(t *testing.T) {
	t.Setenv("DEVSPACE_ARTIFACT_DIR", t.TempDir())
	t.Setenv("DEVSPACE_INLINE_COMMAND_BYTES", "4096")
	root, err := ensureArtifactStore()
	if err != nil {
		t.Fatal(err)
	}
	raw := "alpha\nbeta\n"
	rawPath := filepath.Join(root, "tmp", "small.log")
	if err := os.WriteFile(rawPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	capture, err := finalizeCommandCapture(root, rawPath, "ws_small", "printf", int64(len(raw)), false)
	if err != nil {
		t.Fatal(err)
	}
	if capture.Artifact != nil || capture.Output != "alpha\nbeta" || capture.OmittedLines != 0 {
		t.Fatalf("small output did not preserve v1.7-style inline behavior: %+v", capture)
	}
}

func TestCleanupStaleArtifactTemps(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, ".command-raw-stale")
	fresh := filepath.Join(directory, ".command-redacted-fresh")
	unrelated := filepath.Join(directory, "keep.txt")
	for _, path := range []string{stale, fresh, unrelated} {
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	cleanupStaleArtifactTemps(directory, 30*time.Minute)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale raw artifact temp was not removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh artifact temp was removed")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated temp file was removed")
	}
}

func TestLongSingleLineOutputIsBounded(t *testing.T) {
	longLine := "prefix-error-" + strings.Repeat("x", 10000) + "-tail"
	compact := compactPreviewLine(longLine, 2048)
	if len(compact) > 2300 || !strings.Contains(compact, "bytes omitted within line") || !strings.Contains(compact, "prefix-error-") || !strings.Contains(compact, "-tail") {
		t.Fatalf("long line was not compacted safely: len=%d content=%q", len(compact), compact)
	}
	if !utf8.ValidString(compact) {
		t.Fatal("compacted long line is not valid UTF-8")
	}

	path := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(path, []byte(longLine+"\nshort\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	window, err := readLineWindow(path, 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(window.Content, "bytes omitted within line") || len(window.Content) > 4096 {
		t.Fatalf("line window did not bound a long single line: len=%d\n%s", len(window.Content), window.Content)
	}

	if window.StartLine != 1 || window.EndLine != 2 {
		t.Fatalf("bounded window metadata is inaccurate: start=%d end=%d total=%d", window.StartLine, window.EndLine, window.TotalLines)
	}

	matches, truncated, err := searchOneFile(path, "long.txt", "prefix-error", false, true, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(matches) != 1 || !strings.Contains(matches[0].Excerpt, "bytes omitted within line") || len(matches[0].Excerpt) > 4096 {
		t.Fatalf("search excerpt was not bounded correctly: truncated=%v matches=%+v", truncated, matches)
	}
}

func toolRequest(name string, arguments map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: arguments}}
}
