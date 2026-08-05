package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestReadManyReusesBoundedWorkspaceReads(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{root})
	state := registerWorkspace(root)
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two.txt"), []byte(strings.Repeat("long line\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := handleReadMany(context.Background(), toolRequest("read_many", map[string]any{
		"workspace_id": state.ID,
		"max_bytes":    12 * 1024,
		"files": []any{
			map[string]any{"path": "one.txt", "head": 2},
			map[string]any{"path": "two.txt", "max_lines": 200},
			map[string]any{"path": "missing.txt"},
		},
	}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("read_many failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[readManyView](t, result.StructuredContent)
	if len(view.Files) != 2 || len(view.Errors) != 1 {
		t.Fatalf("unexpected read_many result: %+v", view)
	}
	if view.Files[0].Path != "one.txt" || view.Files[0].StartLine != 1 || view.Files[0].EndLine != 2 || view.Files[0].SHA256 == "" {
		t.Fatalf("first file lost read metadata: %+v", view.Files[0])
	}
	if view.Errors[0].Path != "missing.txt" || view.Errors[0].Error == "" {
		t.Fatalf("missing file error was not preserved: %+v", view.Errors)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 12*1024 {
		t.Fatalf("read_many exceeded aggregate limit: %d bytes", len(encoded))
	}
	if !view.Truncated {
		t.Fatal("aggregate truncation was not reported")
	}
}

func TestTautlineDoctorReportsRuntimeWithoutSecrets(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })
	mcpServer := server.NewMCPServer("test", appVersion, server.WithToolCapabilities(true))
	registerTools(mcpServer)
	app.mcpClients.attachServer(mcpServer)

	previousOwnerToken := ownerToken
	previousGenerated := ownerTokenGenerated
	previousRoots := append([]string(nil), allowedRoots...)
	ownerToken = "doctor-secret-must-not-leak"
	ownerTokenGenerated = false
	allowedRoots = []string{t.TempDir()}
	t.Cleanup(func() {
		ownerToken = previousOwnerToken
		ownerTokenGenerated = previousGenerated
		allowedRoots = previousRoots
	})

	result, err := handleTautlineDoctor(context.Background(), toolRequest("tautline_doctor", nil))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("tautline_doctor failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[doctorView](t, result.StructuredContent)
	if view.Version != appVersion || !view.Running || !view.VersionMatch || view.PublishedTools < 2 {
		t.Fatalf("doctor runtime summary is incomplete: %+v", view)
	}
	if !view.AuthRequired || !view.OwnerTokenConfigured || len(view.AllowedRoots) != 1 {
		t.Fatalf("doctor security summary is incomplete: %+v", view)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), ownerToken) {
		t.Fatal("doctor exposed the owner token")
	}
}

func TestCLIDoctorDetectsActiveVersionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			http.NotFound(writer, request)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": appName,
			"version": "2.7.1",
			"tools":   12,
		})
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	previousRoots := append([]string(nil), allowedRoots...)
	allowedRoots = []string{t.TempDir()}
	t.Cleanup(func() { allowedRoots = previousRoots })

	view := cliDoctorView(newTestConfigStore(t, ""), port)
	if !view.Running || view.VersionMatch || view.RunningVersion != "2.7.1" || !doctorHasErrors(view) {
		t.Fatalf("version mismatch was not diagnosed: %+v", view)
	}
	if !strings.Contains(strings.Join(view.Actions, "\n"), "SWITCH_TO_TAUTLINE.cmd") {
		t.Fatalf("version mismatch action is missing: %+v", view.Actions)
	}
}

func TestReadOnlyConfigLoadDoesNotCreateConfigFile(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "tautline.json")
	t.Setenv("TAUTLINE_CONFIG", configPath)
	t.Setenv("TAUTLINE_RUNTIME_DIR", filepath.Join(root, "runtime"))
	t.Setenv("TAUTLINE_ALLOWED_ROOTS", root)
	store, err := loadTautlineConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if store.path != configPath {
		t.Fatalf("config path=%q, want %q", store.path, configPath)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("read-only config load created %s", configPath)
	}
}

func TestUtilityToolsAreRegistered(t *testing.T) {
	mcpServer := server.NewMCPServer("test", appVersion, server.WithToolCapabilities(true))
	registerTools(mcpServer)
	tools := mcpServer.ListTools()
	for _, name := range []string{"tautline_doctor", "read_many", "exec_command", "write_stdin"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s tool is missing", name)
		}
	}
	for _, name := range []string{"tautline_doctor", "read_many"} {
		encoded, err := json.Marshal(tools[name].Tool)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"readOnlyHint":true`) {
			t.Fatalf("%s is not marked read-only: %s", name, encoded)
		}
	}
	for _, name := range []string{"exec_command", "write_stdin"} {
		encoded, err := json.Marshal(tools[name].Tool)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"readOnlyHint":false`) || !strings.Contains(string(encoded), `"destructiveHint":true`) {
			t.Fatalf("%s process annotations are incomplete: %s", name, encoded)
		}
	}
}
