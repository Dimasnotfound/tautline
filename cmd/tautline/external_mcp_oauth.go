package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

const externalMCPOAuthTimeout = 5 * time.Minute

type externalMCPFileTokenStore struct {
	mu   sync.Mutex
	path string
}

type externalMCPOAuthCallback struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

type externalMCPOAuthResult struct {
	ID            string
	Name          string
	ServerName    string
	ServerVersion string
	ToolCount     int
	TokenFile     string
}

func validateExternalMCPOAuthConfig(serverName string, config *ExternalMCPOAuthConfig) error {
	if config == nil {
		return nil
	}
	if config.ClientID == "" {
		return fmt.Errorf("MCP server %q OAuth requires a client_id", serverName)
	}
	if config.RedirectURI == "" {
		return fmt.Errorf("MCP server %q OAuth requires a redirect_uri", serverName)
	}
	redirect, err := url.Parse(config.RedirectURI)
	if err != nil || redirect.Scheme != "http" || redirect.Host == "" || !isLoopbackMCPHost(redirect.Hostname()) {
		return fmt.Errorf("MCP server %q OAuth redirect_uri must use HTTP on localhost", serverName)
	}
	if redirect.Port() == "" {
		return fmt.Errorf("MCP server %q OAuth redirect_uri requires an explicit port", serverName)
	}
	if redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" {
		return fmt.Errorf("MCP server %q OAuth redirect_uri must not contain credentials, query parameters, or fragments", serverName)
	}
	if redirect.Path == "" || redirect.Path == "/" {
		return fmt.Errorf("MCP server %q OAuth redirect_uri requires a callback path", serverName)
	}
	if len(config.Scopes) == 0 {
		return fmt.Errorf("MCP server %q OAuth requires at least one scope", serverName)
	}
	if config.TokenFile != "" {
		clean := filepath.Clean(config.TokenFile)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("MCP server %q OAuth token_file must be relative to the Tautline runtime directory", serverName)
		}
	}
	if config.AuthServerMetadataURL != "" {
		metadataURL, err := url.Parse(config.AuthServerMetadataURL)
		if err != nil || metadataURL.Host == "" || (metadataURL.Scheme != "https" && !(metadataURL.Scheme == "http" && isLoopbackMCPHost(metadataURL.Hostname()))) {
			return fmt.Errorf("MCP server %q OAuth auth_server_metadata_url is invalid", serverName)
		}
	}
	reserved := map[string]bool{
		"client_id": true, "redirect_uri": true, "response_type": true,
		"scope": true, "state": true, "code_challenge": true, "code_challenge_method": true,
	}
	for key := range config.AuthorizationParams {
		if reserved[strings.ToLower(strings.TrimSpace(key))] {
			return fmt.Errorf("MCP server %q OAuth authorization parameter %q is reserved", serverName, key)
		}
	}
	return nil
}

func addExternalMCPOAuthAuthorizationParams(rawURL string, values map[string]string) (string, error) {
	if len(values) == 0 {
		return rawURL, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, value := range values {
		query.Set(strings.TrimSpace(key), value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (s *externalMCPFileTokenStore) GetToken(ctx context.Context) (*transport.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", transport.ErrNoToken, s.path)
	}
	if err != nil {
		return nil, err
	}
	var token transport.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("decode OAuth token: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" && strings.TrimSpace(token.RefreshToken) == "" {
		return nil, fmt.Errorf("%w: stored OAuth token is empty", transport.ErrNoToken)
	}
	return &token, nil
}

func (s *externalMCPFileTokenStore) SaveToken(ctx context.Context, token *transport.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if token == nil {
		return errors.New("OAuth token is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".oauth-token-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryPath, s.path)
}

func (m *externalMCPManager) externalMCPOAuthTokenPath(config ExternalMCPConfig) (string, error) {
	if config.OAuth == nil {
		return "", errors.New("OAuth is not configured")
	}
	relative := strings.TrimSpace(config.OAuth.TokenFile)
	if relative == "" {
		relative = filepath.Join("oauth", config.ID+".json")
	}
	clean := filepath.Clean(relative)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("OAuth token file must stay inside the Tautline runtime directory")
	}
	return filepath.Join(m.store.snapshot().RuntimeDir, clean), nil
}

func (m *externalMCPManager) externalMCPOAuthClientConfig(config ExternalMCPConfig) (mcpclient.OAuthConfig, string, error) {
	if config.OAuth == nil {
		return mcpclient.OAuthConfig{}, "", errors.New("OAuth is not configured")
	}
	resolved, err := resolveExternalMCPValues(map[string]string{
		"client_id":     config.OAuth.ClientID,
		"client_secret": config.OAuth.ClientSecret,
	})
	if err != nil {
		return mcpclient.OAuthConfig{}, "", err
	}
	tokenPath, err := m.externalMCPOAuthTokenPath(config)
	if err != nil {
		return mcpclient.OAuthConfig{}, "", err
	}
	return mcpclient.OAuthConfig{
		ClientID:              resolved["client_id"],
		ClientSecret:          resolved["client_secret"],
		RedirectURI:           config.OAuth.RedirectURI,
		Scopes:                append([]string(nil), config.OAuth.Scopes...),
		TokenStore:            &externalMCPFileTokenStore{path: tokenPath},
		AuthServerMetadataURL: config.OAuth.AuthServerMetadataURL,
		PKCEEnabled:           true,
	}, tokenPath, nil
}

func (m *externalMCPManager) newExternalMCPHTTPClient(config ExternalMCPConfig, timeout time.Duration) (*mcpclient.Client, error) {
	resolvedHeaders, err := resolveExternalMCPValues(config.Headers)
	if err != nil {
		return nil, err
	}
	options := []transport.StreamableHTTPCOption{
		transport.WithHTTPBasicClient(externalMCPHTTPClient(timeout)),
		transport.WithHTTPHeaders(resolvedHeaders),
		transport.WithHTTPTimeout(timeout),
	}
	if config.OAuth == nil {
		return mcpclient.NewStreamableHttpClient(config.URL, options...)
	}
	oauthConfig, _, err := m.externalMCPOAuthClientConfig(config)
	if err != nil {
		return nil, err
	}
	if _, tokenErr := oauthConfig.TokenStore.GetToken(context.Background()); errors.Is(tokenErr, transport.ErrNoToken) {
		return mcpclient.NewStreamableHttpClient(config.URL, options...)
	} else if tokenErr != nil {
		return nil, fmt.Errorf("read OAuth token: %w", tokenErr)
	}
	return mcpclient.NewOAuthStreamableHttpClient(config.URL, oauthConfig, options...)
}

func (m *externalMCPManager) newExternalMCPSSEClient(config ExternalMCPConfig, timeout time.Duration) (*mcpclient.Client, error) {
	resolvedHeaders, err := resolveExternalMCPValues(config.Headers)
	if err != nil {
		return nil, err
	}
	options := []transport.ClientOption{
		transport.WithHeaders(resolvedHeaders),
		transport.WithHTTPClient(externalMCPHTTPClient(0)),
		transport.WithEndpointTimeout(timeout),
		transport.WithResponseTimeout(timeout),
	}
	if config.OAuth == nil {
		return mcpclient.NewSSEMCPClient(config.URL, options...)
	}
	oauthConfig, _, err := m.externalMCPOAuthClientConfig(config)
	if err != nil {
		return nil, err
	}
	if _, tokenErr := oauthConfig.TokenStore.GetToken(context.Background()); errors.Is(tokenErr, transport.ErrNoToken) {
		return mcpclient.NewSSEMCPClient(config.URL, options...)
	} else if tokenErr != nil {
		return nil, fmt.Errorf("read OAuth token: %w", tokenErr)
	}
	return mcpclient.NewOAuthSSEClient(config.URL, oauthConfig, options...)
}

func (m *externalMCPManager) authorize(ctx context.Context, id string) (externalMCPOAuthResult, error) {
	config, exists := m.config(normalizeMCPToken(id))
	if !exists {
		return externalMCPOAuthResult{}, fmt.Errorf("MCP server %q was not found", id)
	}
	if !isExternalMCPURLTransport(config.Transport) || config.OAuth == nil {
		return externalMCPOAuthResult{}, fmt.Errorf("MCP server %q does not use URL-based OAuth", config.Name)
	}
	oauthConfig, tokenPath, err := m.externalMCPOAuthClientConfig(config)
	if err != nil {
		return externalMCPOAuthResult{}, err
	}
	if _, tokenErr := oauthConfig.TokenStore.GetToken(ctx); tokenErr == nil {
		verifiedClient, serverInfo, tools, verifyErr := m.openClient(ctx, config)
		if verifyErr == nil {
			_ = verifiedClient.Close()
			return externalMCPOAuthResult{ID: config.ID, Name: config.Name, ServerName: serverInfo.ServerInfo.Name, ServerVersion: serverInfo.ServerInfo.Version, ToolCount: len(tools), TokenFile: tokenPath}, nil
		}
	} else if !errors.Is(tokenErr, transport.ErrNoToken) {
		return externalMCPOAuthResult{}, fmt.Errorf("read stored OAuth token: %w", tokenErr)
	}
	handler := transport.NewOAuthHandler(oauthConfig)
	handler.SetBaseURL(config.URL)

	redirect, err := url.Parse(config.OAuth.RedirectURI)
	if err != nil {
		return externalMCPOAuthResult{}, err
	}
	listener, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return externalMCPOAuthResult{}, fmt.Errorf("listen for OAuth callback on %s: %w", redirect.Host, err)
	}
	callbackChannel := make(chan externalMCPOAuthCallback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(redirect.Path, func(w http.ResponseWriter, request *http.Request) {
		callback := externalMCPOAuthCallback{
			Code:             request.URL.Query().Get("code"),
			State:            request.URL.Query().Get("state"),
			Error:            request.URL.Query().Get("error"),
			ErrorDescription: request.URL.Query().Get("error_description"),
		}
		select {
		case callbackChannel <- callback:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>Tautline OAuth</title></head><body style=\"font-family:Arial,sans-serif;padding:32px\"><h1>Google Docs connected</h1><p>You can close this window and return to Tautline.</p></body></html>"))
	})
	callbackServer := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = callbackServer.Serve(listener) }()
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = callbackServer.Shutdown(shutdownContext)
	}()

	codeVerifier, err := mcpclient.GenerateCodeVerifier()
	if err != nil {
		return externalMCPOAuthResult{}, err
	}
	codeChallenge := mcpclient.GenerateCodeChallenge(codeVerifier)
	state, err := mcpclient.GenerateState()
	if err != nil {
		return externalMCPOAuthResult{}, err
	}
	authorizationURL, err := handler.GetAuthorizationURL(ctx, state, codeChallenge)
	if err != nil {
		return externalMCPOAuthResult{}, fmt.Errorf("build OAuth authorization URL: %w", err)
	}
	authorizationURL, err = addExternalMCPOAuthAuthorizationParams(authorizationURL, config.OAuth.AuthorizationParams)
	if err != nil {
		return externalMCPOAuthResult{}, fmt.Errorf("add OAuth authorization parameters: %w", err)
	}
	fmt.Println("Opening Google authorization in the default browser...")
	fmt.Println("If the browser does not open, copy this URL:")
	fmt.Println(authorizationURL)
	if err := openLocalURL(authorizationURL); err != nil {
		fmt.Fprintln(os.Stderr, "browser open warning:", err)
	}

	var callback externalMCPOAuthCallback
	select {
	case callback = <-callbackChannel:
	case <-ctx.Done():
		return externalMCPOAuthResult{}, ctx.Err()
	case <-time.After(externalMCPOAuthTimeout):
		return externalMCPOAuthResult{}, errors.New("OAuth authorization timed out")
	}
	if callback.Error != "" {
		return externalMCPOAuthResult{}, fmt.Errorf("OAuth authorization failed: %s %s", callback.Error, callback.ErrorDescription)
	}
	if callback.State != state {
		return externalMCPOAuthResult{}, errors.New("OAuth callback state did not match")
	}
	if callback.Code == "" {
		return externalMCPOAuthResult{}, errors.New("OAuth callback did not contain an authorization code")
	}
	if err := handler.ProcessAuthorizationResponse(ctx, callback.Code, state, codeVerifier); err != nil {
		return externalMCPOAuthResult{}, fmt.Errorf("exchange OAuth authorization code: %w", err)
	}

	verifiedClient, serverInfo, tools, err := m.openClient(ctx, config)
	if err != nil {
		return externalMCPOAuthResult{}, fmt.Errorf("verify authorized MCP server: %w", err)
	}
	_ = verifiedClient.Close()
	return externalMCPOAuthResult{ID: config.ID, Name: config.Name, ServerName: serverInfo.ServerInfo.Name, ServerVersion: serverInfo.ServerInfo.Version, ToolCount: len(tools), TokenFile: tokenPath}, nil
}

func authorizeExternalMCP(store *configStore, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), externalMCPOAuthTimeout+time.Minute)
	defer cancel()
	result, err := newExternalMCPManager(store).authorize(ctx, id)
	if err != nil {
		return err
	}
	fmt.Println("OAuth authorization successful.")
	fmt.Printf("Connector: %s (%s)\n", result.Name, result.ID)
	fmt.Printf("Server: %s %s\n", result.ServerName, result.ServerVersion)
	fmt.Printf("Tools discovered: %d\n", result.ToolCount)
	fmt.Printf("Token stored: %s\n", result.TokenFile)
	return nil
}
