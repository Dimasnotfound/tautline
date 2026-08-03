package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestValidateExternalMCPOAuthConfig(t *testing.T) {
	valid := &ExternalMCPOAuthConfig{
		ClientID:    "client-id",
		RedirectURI: "http://127.0.0.1:8765/oauth/callback",
		Scopes:      []string{"documents.read"},
		TokenFile:   filepath.Join("oauth", "docs.json"),
	}
	if err := validateExternalMCPOAuthConfig("Docs", valid); err != nil {
		t.Fatalf("valid OAuth config was rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ExternalMCPOAuthConfig)
	}{
		{name: "missing client", mutate: func(config *ExternalMCPOAuthConfig) { config.ClientID = "" }},
		{name: "remote redirect", mutate: func(config *ExternalMCPOAuthConfig) { config.RedirectURI = "https://example.com/oauth/callback" }},
		{name: "missing callback port", mutate: func(config *ExternalMCPOAuthConfig) { config.RedirectURI = "http://127.0.0.1/oauth/callback" }},
		{name: "missing scopes", mutate: func(config *ExternalMCPOAuthConfig) { config.Scopes = nil }},
		{name: "absolute token path", mutate: func(config *ExternalMCPOAuthConfig) { config.TokenFile = filepath.Join(t.TempDir(), "token.json") }},
		{name: "token traversal", mutate: func(config *ExternalMCPOAuthConfig) { config.TokenFile = filepath.Join("..", "token.json") }},
		{name: "reserved authorization parameter", mutate: func(config *ExternalMCPOAuthConfig) {
			config.AuthorizationParams = map[string]string{"redirect_uri": "https://example.com"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyValue := *valid
			copyValue.Scopes = append([]string(nil), valid.Scopes...)
			test.mutate(&copyValue)
			if err := validateExternalMCPOAuthConfig("Docs", &copyValue); err == nil {
				t.Fatal("invalid OAuth config was accepted")
			}
		})
	}
}

func TestAddExternalMCPOAuthAuthorizationParams(t *testing.T) {
	raw, err := addExternalMCPOAuthAuthorizationParams(
		"https://accounts.example.com/auth?client_id=client&state=state",
		map[string]string{"access_type": "offline", "prompt": "consent"},
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("client_id") != "client" || parsed.Query().Get("state") != "state" {
		t.Fatalf("required OAuth parameters changed: %s", raw)
	}
	if parsed.Query().Get("access_type") != "offline" || parsed.Query().Get("prompt") != "consent" {
		t.Fatalf("additional OAuth parameters are missing: %s", raw)
	}
}

func TestExternalMCPFileTokenStorePersistsAndReplacesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "docs.json")
	store := &externalMCPFileTokenStore{path: path}
	if _, err := store.GetToken(context.Background()); !errors.Is(err, transport.ErrNoToken) {
		t.Fatalf("missing token error=%v, want ErrNoToken", err)
	}

	first := &transport.Token{
		AccessToken:  "first-access",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := store.SaveToken(context.Background(), first); err != nil {
		t.Fatalf("save first token: %v", err)
	}
	loaded, err := store.GetToken(context.Background())
	if err != nil {
		t.Fatalf("load first token: %v", err)
	}
	if loaded.AccessToken != first.AccessToken || loaded.RefreshToken != first.RefreshToken {
		t.Fatalf("unexpected first token: %+v", loaded)
	}

	second := &transport.Token{
		AccessToken:  "second-access",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	if err := store.SaveToken(context.Background(), second); err != nil {
		t.Fatalf("replace token: %v", err)
	}
	loaded, err = store.GetToken(context.Background())
	if err != nil {
		t.Fatalf("load replaced token: %v", err)
	}
	if loaded.AccessToken != second.AccessToken {
		t.Fatalf("token was not replaced: %+v", loaded)
	}
}

func TestExternalMCPManagerDiscoversOAuthToolsWithoutStoredToken(t *testing.T) {
	upstream := server.NewMCPServer("public-oauth-docs", "1.0.0", server.WithToolCapabilities(true))
	upstream.AddTool(mcp.NewTool("read_doc"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("document"), nil
	})
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(
		upstream,
		server.WithStateLess(true),
		server.WithDisableStreaming(true),
	))
	defer httpServer.Close()

	manager := newExternalMCPManager(newTestConfigStore(t, ""))
	client, serverInfo, tools, err := manager.openClient(context.Background(), ExternalMCPConfig{
		ID:             "google_docs",
		Name:           "Google Docs",
		Prefix:         "gdocs",
		Transport:      "http",
		URL:            httpServer.URL,
		TimeoutSeconds: 5,
		OAuth: &ExternalMCPOAuthConfig{
			ClientID:    "client-id",
			RedirectURI: "http://127.0.0.1:8765/oauth/callback",
			Scopes:      []string{"documents.read"},
			TokenFile:   filepath.Join("oauth", "missing.json"),
		},
	})
	if err != nil {
		t.Fatalf("discover OAuth tools without token: %v", err)
	}
	defer client.Close()
	if serverInfo.ServerInfo.Name != "public-oauth-docs" || len(tools) != 1 || tools[0].Name != "read_doc" {
		t.Fatalf("unexpected public OAuth discovery: server=%+v tools=%+v", serverInfo.ServerInfo, tools)
	}
}

func TestExternalMCPManagerOpensOAuthHTTPClientWithStoredToken(t *testing.T) {
	upstream := server.NewMCPServer("oauth-docs", "1.0.0", server.WithToolCapabilities(true))
	upstream.AddTool(mcp.NewTool("read_doc"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("document"), nil
	})
	mcpHandler := server.NewStreamableHTTPServer(
		upstream,
		server.WithStateLess(true),
		server.WithDisableStreaming(true),
	)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer stored-access-token" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="test"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, request)
	}))
	defer httpServer.Close()

	configStore := newTestConfigStore(t, "")
	manager := newExternalMCPManager(configStore)
	config := ExternalMCPConfig{
		ID:             "google_docs",
		Name:           "Google Docs",
		Prefix:         "gdocs",
		Transport:      "http",
		URL:            httpServer.URL,
		TimeoutSeconds: 5,
		OAuth: &ExternalMCPOAuthConfig{
			ClientID:    "client-id",
			RedirectURI: "http://127.0.0.1:8765/oauth/callback",
			Scopes:      []string{"documents.read"},
			TokenFile:   filepath.Join("oauth", "google_docs.json"),
		},
	}
	tokenPath, err := manager.externalMCPOAuthTokenPath(config)
	if err != nil {
		t.Fatal(err)
	}
	tokenStore := &externalMCPFileTokenStore{path: tokenPath}
	if err := tokenStore.SaveToken(context.Background(), &transport.Token{
		AccessToken: "stored-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	client, serverInfo, tools, err := manager.openClient(context.Background(), config)
	if err != nil {
		t.Fatalf("open OAuth MCP client: %v", err)
	}
	defer client.Close()
	if serverInfo.ServerInfo.Name != "oauth-docs" || len(tools) != 1 || tools[0].Name != "read_doc" {
		t.Fatalf("unexpected OAuth MCP discovery: server=%+v tools=%+v", serverInfo.ServerInfo, tools)
	}
}
