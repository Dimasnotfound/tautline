package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestPublicLightpandaToolName(t *testing.T) {
	tests := map[string]string{
		"goto":               "lightpanda_goto",
		"detectForms":        "lightpanda_detect_forms",
		"waitForSelector":    "lightpanda_wait_for_selector",
		"session_new":        "lightpanda_session_new",
		"already-kebab.case": "lightpanda_already_kebab_case",
	}
	for native, expected := range tests {
		if actual := publicLightpandaToolName(native); actual != expected {
			t.Fatalf("publicLightpandaToolName(%q)=%q, want %q", native, actual, expected)
		}
	}
}

func TestLightpandaMCPLaunchPersistsStateAndBlocksPrivateNetworks(t *testing.T) {
	runtimeDir := t.TempDir()
	cfg := defaultTautlineConfig().Lightpanda
	cfg.ObeyRobots = true
	cfg.NativeMCP = true
	cfg.PersistSession = true
	cfg.BlockPrivateNetworks = true
	runner := lightpandaRunner{mode: "binary", executable: filepath.Join(runtimeDir, executableName("lightpanda"))}

	launch, err := buildLightpandaMCPLaunch(runner, cfg, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Command != runner.executable {
		t.Fatalf("unexpected command: %q", launch.Command)
	}
	joined := strings.Join(launch.Args, "\n")
	for _, required := range []string{
		"mcp",
		"--obey-robots",
		"--block-private-networks",
		"--cookie-jar",
		filepath.Join(runtimeDir, "state", "lightpanda", "cookies.json"),
		"--storage-engine",
		"sqlite",
		"--storage-sqlite-path",
		filepath.Join(runtimeDir, "state", "lightpanda", "storage.sqlite"),
		"--http-cache-dir",
		filepath.Join(runtimeDir, "cache", "lightpanda"),
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("MCP launch is missing %q: %v", required, launch.Args)
		}
	}
	if strings.Contains(joined, "--cookie\n") {
		t.Fatalf("non-existent cookie input must not be loaded: %v", launch.Args)
	}
	if !containsString(launch.Env, "LIGHTPANDA_DISABLE_TELEMETRY=true") {
		t.Fatalf("telemetry disable environment is missing: %v", launch.Env)
	}

	cookiePath := filepath.Join(runtimeDir, "state", "lightpanda", "cookies.json")
	if err := os.MkdirAll(filepath.Dir(cookiePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookiePath, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	launch, err = buildLightpandaMCPLaunch(runner, cfg, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(launch.Args, "\n")
	if !strings.Contains(joined, "--cookie\n"+cookiePath) {
		t.Fatalf("existing cookie input was not loaded: %v", launch.Args)
	}
}

func TestRegisterLightpandaProxyToolsPreservesSchemaAndForwardsArguments(t *testing.T) {
	native := mcp.NewTool("fill",
		mcp.WithDescription("Fill an element."),
		mcp.WithString("selector", mcp.Required()),
		mcp.WithString("value", mcp.Required()),
	)
	var calledRequest mcp.CallToolRequest
	caller := func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calledRequest = request
		return mcp.NewToolResultText("filled"), nil
	}

	mcpServer := server.NewMCPServer("test", "1.0.0", server.WithToolCapabilities(true))
	if err := registerLightpandaProxyTools(mcpServer, []mcp.Tool{native}, caller); err != nil {
		t.Fatal(err)
	}
	client, err := mcpclient.NewInProcessClient(mcpServer)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	initialize := mcp.InitializeRequest{}
	initialize.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initialize.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	if _, err := client.Initialize(ctx, initialize); err != nil {
		t.Fatal(err)
	}
	listed, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "lightpanda_fill" {
		t.Fatalf("unexpected proxied tools: %+v", listed.Tools)
	}
	schema, err := json.Marshal(listed.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"selector", "value", "required"} {
		if !strings.Contains(string(schema), marker) {
			t.Fatalf("proxied schema lost %q: %s", marker, schema)
		}
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = "lightpanda_fill"
	request.Params.Arguments = map[string]any{"selector": "#username", "value": "admin"}
	request.Params.Meta = mcp.NewMetaFromMap(map[string]any{"session_id": "browser-a"})
	result, err := client.CallTool(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("proxied call returned error: %+v", result)
	}
	if calledRequest.Params.Name != "fill" {
		t.Fatalf("forwarded native name=%q, want fill", calledRequest.Params.Name)
	}
	expectedArguments := map[string]any{"selector": "#username", "value": "admin"}
	if !reflect.DeepEqual(calledRequest.Params.Arguments, expectedArguments) {
		t.Fatalf("forwarded arguments=%#v, want %#v", calledRequest.Params.Arguments, expectedArguments)
	}
	if calledRequest.Params.Meta == nil || calledRequest.Params.Meta.AdditionalFields["session_id"] != "browser-a" {
		t.Fatalf("forwarded MCP metadata was lost: %#v", calledRequest.Params.Meta)
	}
}

func TestLightpandaChildEnvironmentFiltersSecretsAndAllowsScopedValues(t *testing.T) {
	t.Setenv("TAUTLINE_SECRET_VALUE", "must-not-leak")
	t.Setenv("LP_USERNAME", "scoped-user")
	t.Setenv("BRAVE_API_KEY", "scoped-search-key")
	values := lightpandaChildEnvironment([]string{"LIGHTPANDA_DISABLE_TELEMETRY=true"})
	joined := strings.Join(values, "\n")
	if strings.Contains(joined, "TAUTLINE_SECRET_VALUE=") || strings.Contains(joined, "must-not-leak") {
		t.Fatalf("unscoped secret leaked into Lightpanda environment: %s", joined)
	}
	for _, expected := range []string{"LP_USERNAME=scoped-user", "BRAVE_API_KEY=scoped-search-key", "LIGHTPANDA_DISABLE_TELEMETRY=true"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected scoped environment value %q in %s", expected, joined)
		}
	}
}

func TestNativeLightpandaToolSetIncludesInteractiveAndSessionTools(t *testing.T) {
	tools := []mcp.Tool{
		mcp.NewTool("goto"),
		mcp.NewTool("detectForms"),
		mcp.NewTool("click"),
		mcp.NewTool("fill"),
		mcp.NewTool("press"),
		mcp.NewTool("getCookies"),
		mcp.NewTool("session_new"),
		mcp.NewTool("session_list"),
		mcp.NewTool("session_close"),
	}
	public := make([]string, 0, len(tools))
	for _, tool := range tools {
		public = append(public, publicLightpandaToolName(tool.Name))
	}
	sort.Strings(public)
	for _, required := range []string{
		"lightpanda_click",
		"lightpanda_detect_forms",
		"lightpanda_fill",
		"lightpanda_get_cookies",
		"lightpanda_goto",
		"lightpanda_press",
		"lightpanda_session_close",
		"lightpanda_session_list",
		"lightpanda_session_new",
	} {
		index := sort.SearchStrings(public, required)
		if index >= len(public) || public[index] != required {
			t.Fatalf("missing public tool %q in %v", required, public)
		}
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
