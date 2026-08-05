package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestNativeGoogleDocsMigrationDisablesPreviewConnector(t *testing.T) {
	config := defaultTautlineConfig()
	config.MCPServers = []ExternalMCPConfig{{
		ID:             "google_docs",
		Name:           "Google Docs",
		Prefix:         "gdocs",
		Transport:      "streamable-http",
		Enabled:        true,
		URL:            "https://docsmcp.googleapis.com/mcp/v1",
		TimeoutSeconds: 90,
		OAuth: &ExternalMCPOAuthConfig{
			ClientID:    "client-id",
			RedirectURI: "http://127.0.0.1:8765/oauth/callback",
			Scopes:      []string{"https://www.googleapis.com/auth/documents"},
			TokenFile:   filepath.Join("oauth", "google_docs.json"),
		},
	}}

	if err := validateTautlineConfig(&config); err != nil {
		t.Fatal(err)
	}
	if !config.GoogleDocs.Enabled || config.GoogleDocs.OAuth == nil {
		t.Fatalf("native Google Docs migration missing: %+v", config.GoogleDocs)
	}
	if config.MCPServers[0].Enabled {
		t.Fatal("preview Google Docs MCP connector remained enabled after migration")
	}
	config.MCPServers[0].Enabled = true
	if err := validateTautlineConfig(&config); err != nil {
		t.Fatal(err)
	}
	if config.MCPServers[0].Enabled {
		t.Fatal("preview Google Docs MCP connector could be re-enabled beside native tools")
	}
	config.GoogleDocs.OAuth.Scopes[0] = "changed"
	if config.MCPServers[0].OAuth.Scopes[0] == "changed" {
		t.Fatal("migrated OAuth config shares mutable scope storage with the legacy connector")
	}
}

func TestGoogleDocsClientRefreshesTokenAndReadsDocument(t *testing.T) {
	runtimeDir := t.TempDir()
	config := defaultTautlineConfig()
	config.RuntimeDir = runtimeDir
	config.GoogleDocs = GoogleDocsConfig{
		Enabled:        true,
		TimeoutSeconds: 5,
		OAuth: &ExternalMCPOAuthConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			RedirectURI:  "http://127.0.0.1:8765/oauth/callback",
			Scopes:       []string{"https://www.googleapis.com/auth/documents"},
			TokenFile:    filepath.Join("oauth", "google_docs.json"),
		},
	}
	if err := validateTautlineConfig(&config); err != nil {
		t.Fatal(err)
	}
	store := &configStore{path: filepath.Join(runtimeDir, "config", "tautline.json"), value: config}
	tokenPath, err := googleDocsTokenPath(runtimeDir, config.GoogleDocs)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&externalMCPFileTokenStore{path: tokenPath}).SaveToken(context.Background(), &transport.Token{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenCalls++
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_id") != "client-id" || request.Form.Get("client_secret") != "client-secret" || request.Form.Get("refresh_token") != "refresh-token" {
				t.Fatalf("unexpected refresh form: %s", request.Form.Encode())
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"access_token":"fresh-token","token_type":"Bearer","expires_in":3600,"scope":"docs"}`))
		case "/v1/documents/doc-1":
			if request.Header.Get("Authorization") != "Bearer fresh-token" {
				t.Fatalf("authorization=%q", request.Header.Get("Authorization"))
			}
			if request.URL.Query().Get("includeTabsContent") != "true" {
				t.Fatalf("includeTabsContent missing: %s", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"documentId":"doc-1","title":"Native Docs"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := newGoogleDocsClient(store)
	if err != nil {
		t.Fatal(err)
	}
	client.apiBaseURL = server.URL + "/v1"
	client.tokenURL = server.URL + "/token"
	client.httpClient = server.Client()
	body, err := client.readDocument(context.Background(), "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 || !strings.Contains(string(body), `"title":"Native Docs"`) {
		t.Fatalf("tokenCalls=%d body=%s", tokenCalls, body)
	}
	stored, err := (&externalMCPFileTokenStore{path: tokenPath}).GetToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "fresh-token" || stored.RefreshToken != "refresh-token" || stored.ExpiresAt.IsZero() {
		t.Fatalf("unexpected persisted token: %+v", stored)
	}
}

func TestGoogleDocsClientSendsBatchUpdateAndRegistersNativeTools(t *testing.T) {
	runtimeDir := t.TempDir()
	config := defaultTautlineConfig()
	config.RuntimeDir = runtimeDir
	config.GoogleDocs = GoogleDocsConfig{
		Enabled:        true,
		TimeoutSeconds: 5,
		OAuth: &ExternalMCPOAuthConfig{
			ClientID:    "client-id",
			RedirectURI: "http://127.0.0.1:8765/oauth/callback",
			Scopes:      []string{"https://www.googleapis.com/auth/documents"},
			TokenFile:   filepath.Join("oauth", "google_docs.json"),
		},
	}
	if err := validateTautlineConfig(&config); err != nil {
		t.Fatal(err)
	}
	store := &configStore{path: filepath.Join(runtimeDir, "config", "tautline.json"), value: config}
	tokenPath, err := googleDocsTokenPath(runtimeDir, config.GoogleDocs)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&externalMCPFileTokenStore{path: tokenPath}).SaveToken(context.Background(), &transport.Token{
		AccessToken: "access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/documents/doc-2:batchUpdate" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization=%q", request.Header.Get("Authorization"))
		}
		var payload struct {
			Requests []map[string]any `json:"requests"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Requests) != 1 || payload.Requests[0]["insertText"] == nil {
			t.Fatalf("unexpected batch update payload: %+v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"documentId":"doc-2","replies":[{}]}`))
	}))
	defer server.Close()

	client, err := newGoogleDocsClient(store)
	if err != nil {
		t.Fatal(err)
	}
	client.apiBaseURL = server.URL + "/v1"
	client.httpClient = server.Client()
	result, err := client.updateDocument(context.Background(), "doc-2", []any{
		map[string]any{"insertText": map[string]any{"text": "halo", "endOfSegmentLocation": map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), `"documentId":"doc-2"`) {
		t.Fatalf("unexpected result: %s", result)
	}

	mcpServer := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
	registerGoogleDocsTools(mcpServer, store)
	tools := mcpServer.ListTools()
	if _, exists := tools["gdocs_read_doc"]; !exists {
		t.Fatal("gdocs_read_doc tool is missing")
	}
	if _, exists := tools["gdocs_update_doc"]; !exists {
		t.Fatal("gdocs_update_doc tool is missing")
	}

	parsed, err := url.Parse(server.URL)
	if err != nil || parsed.Scheme != "http" {
		t.Fatalf("test server URL invalid: %s", server.URL)
	}
}
