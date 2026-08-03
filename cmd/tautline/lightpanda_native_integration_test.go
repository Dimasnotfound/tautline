package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestInstalledLightpandaNativeMCPTools(t *testing.T) {
	if os.Getenv("TAUTLINE_TEST_LIGHTPANDA_MCP") != "1" {
		t.Skip("set TAUTLINE_TEST_LIGHTPANDA_MCP=1 to probe the installed Lightpanda MCP server")
	}

	executable := os.Getenv("TAUTLINE_TEST_LIGHTPANDA_PATH")
	if executable == "" {
		root, err := filepath.Abs(filepath.Join("..", "..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		executable = filepath.Join(root, "bin", executableName("lightpanda"))
	}

	client, err := mcpclient.NewStdioMCPClient(executable, []string{"LIGHTPANDA_DISABLE_TELEMETRY=true"}, "mcp", "--log-level", "error")
	if err != nil {
		t.Fatalf("start Lightpanda MCP: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "tautline-test", Version: "2.2.0"}
	if _, err := client.Initialize(ctx, request); err != nil {
		t.Fatalf("initialize Lightpanda MCP: %v", err)
	}
	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list Lightpanda MCP tools: %v", err)
	}
	assertInteractiveLightpandaTools(t, result.Tools)
	t.Logf("Lightpanda MCP exposed %d native tools", len(result.Tools))
}

func TestTautlineLightpandaManagerNavigatesWithPersistentNativeMCP(t *testing.T) {
	if os.Getenv("TAUTLINE_TEST_LIGHTPANDA_MCP") != "1" {
		t.Skip("set TAUTLINE_TEST_LIGHTPANDA_MCP=1 to run the installed Lightpanda manager integration test")
	}
	executable := strings.TrimSpace(os.Getenv("TAUTLINE_TEST_LIGHTPANDA_PATH"))
	if executable == "" {
		t.Fatal("TAUTLINE_TEST_LIGHTPANDA_PATH must point to the v2.2.0 Lightpanda shim for this test")
	}
	runtimeDir := t.TempDir()
	cfg := defaultTautlineConfig()
	cfg.RuntimeDir = runtimeDir
	cfg.Lightpanda.Executable = executable
	cfg.Lightpanda.NativeMCP = true
	cfg.Lightpanda.PersistSession = true
	cfg.Lightpanda.BlockPrivateNetworks = true
	cfg.Lightpanda.NativeTimeoutSeconds = 45
	store := &configStore{path: filepath.Join(runtimeDir, "config", "tautline.json"), value: cfg}
	manager := newLightpandaManager(store)
	defer manager.closeNativeMCP()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tools, err := manager.prepareNativeMCP(ctx)
	if err != nil {
		t.Fatalf("prepare Tautline Lightpanda MCP: %v", err)
	}
	assertInteractiveLightpandaTools(t, tools)

	gotoResult, err := manager.callNativeTool(ctx, "goto", map[string]any{"url": "https://example.com", "timeout": 20000})
	if err != nil {
		t.Fatalf("Lightpanda goto failed: %v", err)
	}
	if gotoResult.IsError {
		t.Fatalf("Lightpanda goto returned an MCP error: %s", lightpandaResultText(gotoResult))
	}
	urlResult, err := manager.callNativeTool(ctx, "getUrl", map[string]any{})
	if err != nil {
		t.Fatalf("Lightpanda getUrl failed: %v", err)
	}
	if urlResult.IsError || !strings.Contains(lightpandaResultText(urlResult), "example.com") {
		t.Fatalf("unexpected Lightpanda URL result: %s", lightpandaResultText(urlResult))
	}
	ready, toolCount, _, _, nativeError := manager.nativeMCPStatus()
	if !ready || toolCount < 25 || nativeError != "" {
		t.Fatalf("unexpected manager status ready=%v tools=%d error=%q", ready, toolCount, nativeError)
	}
	for _, expected := range []string{
		filepath.Join(runtimeDir, "state", "lightpanda"),
		filepath.Join(runtimeDir, "cache", "lightpanda"),
	} {
		if info, err := os.Stat(expected); err != nil || !info.IsDir() {
			t.Fatalf("persistent Lightpanda directory was not created: %s", expected)
		}
	}
}

func TestRunningTautlineExportsNativeLightpandaTools(t *testing.T) {
	serverURL := strings.TrimRight(strings.TrimSpace(os.Getenv("TAUTLINE_TEST_SERVER_URL")), "/")
	if serverURL == "" {
		t.Skip("set TAUTLINE_TEST_SERVER_URL to probe a running Tautline MCP server")
	}
	client, err := mcpclient.NewStreamableHttpClient(serverURL + "/mcp")
	if err != nil {
		t.Fatalf("create Tautline HTTP MCP client: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start Tautline HTTP MCP client: %v", err)
	}
	initialize := mcp.InitializeRequest{}
	initialize.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initialize.Params.ClientInfo = mcp.Implementation{Name: "tautline-http-smoke", Version: "2.2.0"}
	if _, err := client.Initialize(ctx, initialize); err != nil {
		t.Fatalf("initialize Tautline HTTP MCP client: %v", err)
	}
	listed, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list Tautline HTTP MCP tools: %v", err)
	}
	available := make(map[string]bool, len(listed.Tools))
	for _, tool := range listed.Tools {
		available[tool.Name] = true
	}
	for _, required := range []string{"lightpanda_goto", "lightpanda_detect_forms", "lightpanda_click", "lightpanda_fill", "lightpanda_press", "lightpanda_select_option", "lightpanda_set_checked", "lightpanda_get_cookies", "lightpanda_session_new", "lightpanda_session_list", "lightpanda_session_close"} {
		if !available[required] {
			t.Fatalf("running Tautline server is missing public tool %q", required)
		}
	}
	request := mcp.CallToolRequest{}
	request.Params.Name = "lightpanda_goto"
	request.Params.Arguments = map[string]any{"url": "https://example.com", "timeout": 20000}
	result, err := client.CallTool(ctx, request)
	if err != nil {
		t.Fatalf("call proxied lightpanda_goto: %v", err)
	}
	if result.IsError {
		t.Fatalf("proxied lightpanda_goto returned an MCP error: %s", lightpandaResultText(result))
	}
	t.Logf("running Tautline exposed %d total tools with native Lightpanda interaction", len(listed.Tools))
}

func assertInteractiveLightpandaTools(t *testing.T, tools []mcp.Tool) {
	t.Helper()
	if len(tools) < 25 {
		t.Fatalf("Lightpanda MCP returned only %d tools", len(tools))
	}
	available := make(map[string]bool, len(tools))
	for _, tool := range tools {
		available[tool.Name] = true
	}
	for _, required := range []string{"goto", "detectForms", "click", "fill", "press", "selectOption", "setChecked", "getCookies", "session_new", "session_list", "session_close"} {
		if !available[required] {
			t.Fatalf("Lightpanda MCP is missing required tool %q", required)
		}
	}
}

func lightpandaResultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			parts = append(parts, text.Text)
			continue
		}
		encoded, _ := json.Marshal(content)
		parts = append(parts, string(encoded))
	}
	return strings.Join(parts, "\n")
}
