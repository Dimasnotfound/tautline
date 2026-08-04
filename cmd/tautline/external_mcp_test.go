package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestNormalizeExternalMCPConfigs(t *testing.T) {
	configs := []ExternalMCPConfig{{
		Name:      "Google Docs",
		Transport: "stdio",
		Command:   "npx.cmd",
		Args:      []string{" -y ", "@example/google-docs-mcp"},
	}}
	if err := normalizeExternalMCPConfigs(&configs); err != nil {
		t.Fatalf("normalize connector: %v", err)
	}
	config := configs[0]
	if config.ID != "mcp_google_docs" || config.Prefix != "google_docs" {
		t.Fatalf("unexpected normalized identity: %+v", config)
	}
	if config.TimeoutSeconds != defaultExternalMCPTimeout {
		t.Fatalf("timeout=%d, want %d", config.TimeoutSeconds, defaultExternalMCPTimeout)
	}
	if len(config.Args) != 2 || config.Args[0] != "-y" {
		t.Fatalf("arguments were not compacted: %#v", config.Args)
	}
}

func TestNormalizeExternalMCPConfigsRejectsUnsafeHTTP(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "local HTTP", url: "http://127.0.0.1:9123/mcp"},
		{name: "remote HTTPS", url: "https://mcp.example.com/mcp"},
		{name: "remote HTTP", url: "http://mcp.example.com/mcp", wantErr: true},
		{name: "query secret", url: "https://mcp.example.com/mcp?token=value", wantErr: true},
		{name: "embedded credentials", url: "https://user:password@mcp.example.com/mcp", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configs := []ExternalMCPConfig{{
				Name:      "Remote",
				Transport: "http",
				URL:       test.url,
			}}
			err := normalizeExternalMCPConfigs(&configs)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalize error=%v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestNormalizeExternalMCPURLMigratesOnlyLegacyGoogleDocsEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "https://docsmcp.googleapis.com/mcp", want: "https://docsmcp.googleapis.com/mcp/v1"},
		{input: "https://docsmcp.googleapis.com/mcp/", want: "https://docsmcp.googleapis.com/mcp/v1"},
		{input: "https://docsmcp.googleapis.com/mcp/v1", want: "https://docsmcp.googleapis.com/mcp/v1"},
		{input: "https://mcp.example.com/mcp", want: "https://mcp.example.com/mcp"},
	}
	for _, test := range tests {
		if got := normalizeExternalMCPURL(test.input); got != test.want {
			t.Fatalf("normalize URL %q=%q, want %q", test.input, got, test.want)
		}
	}
}

func TestResolveExternalMCPValues(t *testing.T) {
	t.Setenv("TAUTLINE_TEST_TOKEN", "secret-value")
	resolved, err := resolveExternalMCPValues(map[string]string{
		"Authorization": "Bearer ${TAUTLINE_TEST_TOKEN}",
	})
	if err != nil {
		t.Fatalf("resolve environment reference: %v", err)
	}
	if resolved["Authorization"] != "Bearer secret-value" {
		t.Fatalf("unexpected resolved value: %q", resolved["Authorization"])
	}
	if _, err := resolveExternalMCPValues(map[string]string{"Authorization": "${TAUTLINE_MISSING_TOKEN}"}); err == nil {
		t.Fatal("missing environment reference was accepted")
	}
}

func TestBuildExternalMCPToolsPrefixesAndForwards(t *testing.T) {
	native := mcp.NewTool("readDocument",
		mcp.WithDescription("Read one document."),
		mcp.WithString("document_id", mcp.Required()),
	)
	var forwarded mcp.CallToolRequest
	names, entries, err := buildExternalMCPTools(
		ExternalMCPConfig{Name: "Google Docs", Prefix: "gdocs"},
		[]mcp.Tool{native},
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			forwarded = request
			return mcp.NewToolResultText("document"), nil
		},
	)
	if err != nil {
		t.Fatalf("build proxy tools: %v", err)
	}
	if len(names) != 1 || names[0] != "gdocs_read_document" || len(entries) != 1 {
		t.Fatalf("unexpected public tools: names=%v entries=%d", names, len(entries))
	}
	request := toolRequest(names[0], map[string]any{"document_id": "doc-1"})
	if _, err := entries[0].Handler(context.Background(), request); err != nil {
		t.Fatalf("call proxy handler: %v", err)
	}
	if forwarded.Params.Name != "readDocument" {
		t.Fatalf("forwarded tool=%q, want readDocument", forwarded.Params.Name)
	}
	arguments, ok := forwarded.Params.Arguments.(map[string]any)
	if !ok || arguments["document_id"] != "doc-1" {
		t.Fatalf("arguments were not forwarded: %#v", forwarded.Params.Arguments)
	}
}

func TestNormalizeExternalMCPTransportAliases(t *testing.T) {
	tests := map[string]string{
		"http":            externalMCPTransportStreamableHTTP,
		"streamable_http": externalMCPTransportStreamableHTTP,
		"legacy-sse":      externalMCPTransportSSE,
		"automatic":       externalMCPTransportAuto,
		"stdio":           externalMCPTransportStdio,
	}
	for input, want := range tests {
		if got := normalizeExternalMCPTransport(input); got != want {
			t.Fatalf("normalize transport %q=%q, want %q", input, got, want)
		}
	}
}

func TestExternalMCPManagerOpensHTTPClient(t *testing.T) {
	upstream := server.NewMCPServer("mock-docs", "1.0.0", server.WithToolCapabilities(true))
	upstream.AddTool(mcp.NewTool("ping"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("pong"), nil
	})
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(
		upstream,
		server.WithStateLess(true),
		server.WithDisableStreaming(true),
	))
	defer httpServer.Close()

	manager := &externalMCPManager{}
	client, serverInfo, tools, err := manager.openClient(context.Background(), ExternalMCPConfig{
		Name:           "Mock Docs",
		Transport:      "http",
		URL:            httpServer.URL,
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("open HTTP MCP client: %v", err)
	}
	defer client.Close()
	if serverInfo.ServerInfo.Name != "mock-docs" || len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("unexpected MCP discovery: server=%+v tools=%+v", serverInfo.ServerInfo, tools)
	}
	request := toolRequest("ping", map[string]any{})
	if _, err := client.CallTool(context.Background(), request); err != nil {
		t.Fatalf("call discovered MCP tool: %v", err)
	}
}

func TestExternalMCPManagerOpensLegacySSEClient(t *testing.T) {
	upstream := server.NewMCPServer("legacy-docs", "1.0.0", server.WithToolCapabilities(true))
	upstream.AddTool(mcp.NewTool("ping"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("pong"), nil
	})
	testServer := server.NewTestServer(upstream)
	defer testServer.Close()

	manager := &externalMCPManager{}
	client, serverInfo, tools, err := manager.openClient(context.Background(), ExternalMCPConfig{
		Name:           "Legacy Docs",
		Transport:      externalMCPTransportSSE,
		URL:            testServer.URL + "/sse",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("open SSE MCP client: %v", err)
	}
	defer client.Close()
	if serverInfo.ServerInfo.Name != "legacy-docs" || len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("unexpected SSE discovery: server=%+v tools=%+v", serverInfo.ServerInfo, tools)
	}
}

func TestExternalMCPAutoFallsBackToLegacySSE(t *testing.T) {
	upstream := server.NewMCPServer("auto-legacy-docs", "1.0.0", server.WithToolCapabilities(true))
	upstream.AddTool(mcp.NewTool("ping"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("pong"), nil
	})
	testServer := server.NewTestServer(upstream)
	defer testServer.Close()

	manager := &externalMCPManager{}
	client, serverInfo, tools, err := manager.openClient(context.Background(), ExternalMCPConfig{
		Name:           "Auto Legacy Docs",
		Transport:      externalMCPTransportAuto,
		URL:            testServer.URL + "/sse",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("auto fallback to SSE: %v", err)
	}
	defer client.Close()
	if serverInfo.ServerInfo.Name != "auto-legacy-docs" || len(tools) != 1 {
		t.Fatalf("unexpected auto SSE discovery: server=%+v tools=%+v", serverInfo.ServerInfo, tools)
	}
}

func TestExternalMCPStatusDoesNotExposeSecretValues(t *testing.T) {
	status := statusFromExternalMCPConfig(ExternalMCPConfig{
		ID:          "mcp_docs",
		Name:        "Docs",
		Prefix:      "docs",
		Transport:   "http",
		URL:         "https://mcp.example.com/mcp",
		Args:        []string{"--token", "top-secret"},
		Environment: map[string]string{"API_TOKEN": "top-secret"},
		Headers:     map[string]string{"Authorization": "Bearer top-secret"},
		OAuth: &ExternalMCPOAuthConfig{
			ClientID:     "public-client-id",
			ClientSecret: "top-secret",
			RedirectURI:  "http://127.0.0.1:8765/oauth/callback",
			Scopes:       []string{"documents.read"},
		},
	})
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(encoded), "top-secret") {
		t.Fatalf("status exposed a secret value: %s", encoded)
	}
	if status.ArgumentCount != 2 || len(status.EnvironmentKeys) != 1 || len(status.HeaderKeys) != 1 {
		t.Fatalf("status lost safe metadata: %+v", status)
	}
}
