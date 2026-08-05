package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	googleDocsAPIBaseURL        = "https://docs.googleapis.com/v1"
	googleOAuthAuthorizationURL = "https://accounts.google.com/o/oauth2/v2/auth"
	googleOAuthTokenURL         = "https://oauth2.googleapis.com/token"
	googleDocsResponseLimit     = 16 << 20
	googleDocsErrorLimit        = 64 << 10
)

type GoogleDocsConfig struct {
	Enabled        bool                    `json:"enabled"`
	OAuth          *ExternalMCPOAuthConfig `json:"oauth,omitempty"`
	TimeoutSeconds int                     `json:"timeout_seconds"`
}

type googleDocsClient struct {
	config     GoogleDocsConfig
	tokenPath  string
	apiBaseURL string
	tokenURL   string
	httpClient *http.Client
	mu         sync.Mutex
}

type googleOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

type googleDocsHealthView struct {
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	TokenStored bool   `json:"token_stored"`
}

func cloneExternalMCPOAuthConfig(config *ExternalMCPOAuthConfig) *ExternalMCPOAuthConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	cloned.Scopes = append([]string(nil), config.Scopes...)
	cloned.AuthorizationParams = cloneStringMap(config.AuthorizationParams)
	return &cloned
}

func isOfficialGoogleDocsMCPConfig(config ExternalMCPConfig) bool {
	if config.OAuth == nil || normalizeMCPToken(config.ID) != "google_docs" {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(config.URL))
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "docsmcp.googleapis.com")
}

func migrateNativeGoogleDocsConfig(config *TautlineConfig) {
	for index := range config.MCPServers {
		legacy := &config.MCPServers[index]
		if !isOfficialGoogleDocsMCPConfig(*legacy) {
			continue
		}
		if config.GoogleDocs.OAuth == nil {
			config.GoogleDocs = GoogleDocsConfig{
				Enabled:        legacy.Enabled,
				OAuth:          cloneExternalMCPOAuthConfig(legacy.OAuth),
				TimeoutSeconds: legacy.TimeoutSeconds,
			}
		}
		if config.GoogleDocs.Enabled {
			legacy.Enabled = false
		}
	}
}

func googleDocsTokenPath(runtimeDir string, config GoogleDocsConfig) (string, error) {
	if config.OAuth == nil {
		return "", errors.New("Google Docs OAuth is not configured")
	}
	relative := strings.TrimSpace(config.OAuth.TokenFile)
	if relative == "" {
		relative = filepath.Join("oauth", "google_docs.json")
	}
	clean := filepath.Clean(relative)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("Google Docs OAuth token file must stay inside the Tautline runtime directory")
	}
	return filepath.Join(runtimeDir, clean), nil
}

func newGoogleDocsClient(store *configStore) (*googleDocsClient, error) {
	config := store.snapshot().GoogleDocs
	if !config.Enabled {
		return nil, errors.New("native Google Docs integration is disabled")
	}
	if config.OAuth == nil {
		return nil, errors.New("native Google Docs OAuth is not configured")
	}
	resolved, err := resolveExternalMCPValues(map[string]string{
		"client_id":     config.OAuth.ClientID,
		"client_secret": config.OAuth.ClientSecret,
	})
	if err != nil {
		return nil, err
	}
	config.OAuth = cloneExternalMCPOAuthConfig(config.OAuth)
	config.OAuth.ClientID = resolved["client_id"]
	config.OAuth.ClientSecret = resolved["client_secret"]
	tokenPath, err := googleDocsTokenPath(store.snapshot().RuntimeDir, config)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	return &googleDocsClient{
		config:     config,
		tokenPath:  tokenPath,
		apiBaseURL: googleDocsAPIBaseURL,
		tokenURL:   googleOAuthTokenURL,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (c *googleDocsClient) accessToken(ctx context.Context, forceRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	store := &externalMCPFileTokenStore{path: c.tokenPath}
	token, err := store.GetToken(ctx)
	if err != nil {
		if errors.Is(err, transport.ErrNoToken) {
			return "", errors.New("Google Docs is not authorized; run tautline -auth-google-docs")
		}
		return "", err
	}
	if !forceRefresh && strings.TrimSpace(token.AccessToken) != "" && (token.ExpiresAt.IsZero() || time.Until(token.ExpiresAt) > time.Minute) {
		return token.AccessToken, nil
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		return "", errors.New("Google Docs refresh token is missing; run tautline -auth-google-docs")
	}

	form := url.Values{
		"client_id":     {c.config.OAuth.ClientID},
		"refresh_token": {token.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	if c.config.OAuth.ClientSecret != "" {
		form.Set("client_secret", c.config.OAuth.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("refresh Google Docs OAuth token: %w", err)
	}
	defer response.Body.Close()
	body, err := readBoundedBody(response.Body, googleDocsErrorLimit)
	if err != nil {
		return "", err
	}
	var refreshed googleOAuthTokenResponse
	if err := json.Unmarshal(body, &refreshed); err != nil {
		return "", fmt.Errorf("decode Google OAuth response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || refreshed.AccessToken == "" {
		message := strings.TrimSpace(strings.Join([]string{refreshed.Error, refreshed.Description}, ": "))
		if message == ":" || message == "" {
			message = strings.TrimSpace(string(body))
		}
		return "", fmt.Errorf("Google OAuth token refresh returned %s: %s", response.Status, message)
	}

	updated := &transport.Token{
		AccessToken:  refreshed.AccessToken,
		TokenType:    refreshed.TokenType,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    refreshed.ExpiresIn,
		Scope:        refreshed.Scope,
	}
	if updated.TokenType == "" {
		updated.TokenType = token.TokenType
	}
	if updated.Scope == "" {
		updated.Scope = token.Scope
	}
	if refreshed.RefreshToken != "" {
		updated.RefreshToken = refreshed.RefreshToken
	}
	if refreshed.ExpiresIn > 0 {
		updated.ExpiresAt = time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
	}
	if err := store.SaveToken(ctx, updated); err != nil {
		return "", fmt.Errorf("save refreshed Google Docs OAuth token: %w", err)
	}
	return updated.AccessToken, nil
}

func readBoundedBody(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("Google response exceeded %d bytes", limit)
	}
	return body, nil
}

func (c *googleDocsClient) requestJSON(ctx context.Context, method, endpoint string, payload []byte) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.accessToken(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept", "application/json")
		if len(payload) > 0 {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("call Google Docs API: %w", err)
		}
		limit := int64(googleDocsResponseLimit)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			limit = googleDocsErrorLimit
		}
		body, readErr := readBoundedBody(response.Body, limit)
		_ = response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("Google Docs API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
		}
		if !json.Valid(body) {
			return nil, errors.New("Google Docs API returned invalid JSON")
		}
		return body, nil
	}
	return nil, errors.New("Google Docs API authorization failed after token refresh")
}

func (c *googleDocsClient) readDocument(ctx context.Context, documentID string) ([]byte, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, errors.New("documentId is required")
	}
	endpoint := strings.TrimRight(c.apiBaseURL, "/") + "/documents/" + url.PathEscape(documentID) + "?includeTabsContent=true"
	return c.requestJSON(ctx, http.MethodGet, endpoint, nil)
}

func (c *googleDocsClient) updateDocument(ctx context.Context, documentID string, requests []any) ([]byte, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, errors.New("documentId is required")
	}
	if len(requests) == 0 {
		return nil, errors.New("requests must contain at least one Google Docs batchUpdate request")
	}
	payload, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.apiBaseURL, "/") + "/documents/" + url.PathEscape(documentID) + ":batchUpdate"
	return c.requestJSON(ctx, http.MethodPost, endpoint, payload)
}

func registerGoogleDocsTools(mcpServer *server.MCPServer, store *configStore) {
	if !store.snapshot().GoogleDocs.Enabled {
		return
	}
	readTool := mcp.NewTool("gdocs_read_doc",
		mcp.WithTitleAnnotation("Read Google Doc"),
		mcp.WithDescription("Retrieve the JSON representation of a Google Doc directly through the stable Google Docs REST API. The result includes document text and structure."),
		mcp.WithString("documentId", mcp.Required(), mcp.Description("Google Docs document ID")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	mcpServer.AddTool(readTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := newGoogleDocsClient(store)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		documentID, err := request.RequireString("documentId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result, err := client.readDocument(ctx, documentID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(result)), nil
	})

	updateTool := mcp.NewTool("gdocs_update_doc",
		mcp.WithTitleAnnotation("Update Google Doc"),
		mcp.WithDescription("Apply Google Docs documents.batchUpdate requests directly through the stable Google Docs REST API. Read the document first, apply the smallest update, then read it again to verify the result."),
		mcp.WithString("documentId", mcp.Required(), mcp.Description("Google Docs document ID")),
		mcp.WithArray("requests", mcp.Required(), mcp.Description("Google Docs documents.batchUpdate request objects"), mcp.Items(map[string]any{"type": "object"})),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	mcpServer.AddTool(updateTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := newGoogleDocsClient(store)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		documentID, err := request.RequireString("documentId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		rawRequests, ok := request.GetArguments()["requests"].([]any)
		if !ok {
			return mcp.NewToolResultError("requests must be an array of Google Docs batchUpdate request objects"), nil
		}
		result, err := client.updateDocument(ctx, documentID, rawRequests)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(result)), nil
	})
}

func googleDocsHealth(store *configStore) googleDocsHealthView {
	config := store.snapshot().GoogleDocs
	view := googleDocsHealthView{Enabled: config.Enabled, Mode: "native-rest"}
	if tokenPath, err := googleDocsTokenPath(store.snapshot().RuntimeDir, config); err == nil {
		if info, statErr := os.Stat(tokenPath); statErr == nil && !info.IsDir() {
			view.TokenStored = true
		}
	}
	return view
}

func authorizeGoogleDocs(store *configStore) error {
	client, err := newGoogleDocsClient(store)
	if err != nil {
		return err
	}
	config := client.config.OAuth
	redirect, err := url.Parse(config.RedirectURI)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return fmt.Errorf("listen for Google OAuth callback on %s: %w", redirect.Host, err)
	}
	callbackChannel := make(chan externalMCPOAuthCallback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(redirect.Path, func(writer http.ResponseWriter, request *http.Request) {
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
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<!doctype html><html><body style=\"font-family:Arial,sans-serif;padding:32px\"><h1>Google Docs connected</h1><p>You can close this window and return to Tautline.</p></body></html>"))
	})
	callbackServer := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = callbackServer.Serve(listener) }()
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = callbackServer.Shutdown(shutdownContext)
	}()

	verifier, err := mcpclient.GenerateCodeVerifier()
	if err != nil {
		return err
	}
	challenge := mcpclient.GenerateCodeChallenge(verifier)
	state, err := mcpclient.GenerateState()
	if err != nil {
		return err
	}
	query := url.Values{
		"client_id":             {config.ClientID},
		"redirect_uri":          {config.RedirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(config.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	for key, value := range config.AuthorizationParams {
		query.Set(key, value)
	}
	authorizationURL := googleOAuthAuthorizationURL + "?" + query.Encode()
	fmt.Println("Opening Google authorization in the default browser...")
	fmt.Println("If the browser does not open, copy this URL:")
	fmt.Println(authorizationURL)
	if err := openLocalURL(authorizationURL); err != nil {
		fmt.Fprintln(os.Stderr, "browser open warning:", err)
	}

	var callback externalMCPOAuthCallback
	select {
	case callback = <-callbackChannel:
	case <-time.After(externalMCPOAuthTimeout):
		return errors.New("Google OAuth authorization timed out")
	}
	if callback.Error != "" {
		return fmt.Errorf("Google OAuth authorization failed: %s %s", callback.Error, callback.ErrorDescription)
	}
	if callback.State != state {
		return errors.New("Google OAuth callback state did not match")
	}
	if callback.Code == "" {
		return errors.New("Google OAuth callback did not contain an authorization code")
	}

	form := url.Values{
		"client_id":     {config.ClientID},
		"code":          {callback.Code},
		"code_verifier": {verifier},
		"redirect_uri":  {config.RedirectURI},
		"grant_type":    {"authorization_code"},
	}
	if config.ClientSecret != "" {
		form.Set("client_secret", config.ClientSecret)
	}
	request, err := http.NewRequest(http.MethodPost, client.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := readBoundedBody(response.Body, googleDocsErrorLimit)
	if err != nil {
		return err
	}
	var tokenResponse googleOAuthTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return fmt.Errorf("decode Google OAuth token: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || tokenResponse.AccessToken == "" {
		return fmt.Errorf("Google OAuth token exchange returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	refreshToken := tokenResponse.RefreshToken
	if refreshToken == "" {
		if previous, previousErr := (&externalMCPFileTokenStore{path: client.tokenPath}).GetToken(context.Background()); previousErr == nil {
			refreshToken = previous.RefreshToken
		}
	}
	token := &transport.Token{
		AccessToken:  tokenResponse.AccessToken,
		TokenType:    tokenResponse.TokenType,
		RefreshToken: refreshToken,
		ExpiresIn:    tokenResponse.ExpiresIn,
		Scope:        tokenResponse.Scope,
	}
	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if err := (&externalMCPFileTokenStore{path: client.tokenPath}).SaveToken(context.Background(), token); err != nil {
		return err
	}
	fmt.Printf("Google Docs OAuth authorization successful. Token stored: %s\n", client.tokenPath)
	return nil
}

func testGoogleDocs(store *configStore, documentID string) error {
	client, err := newGoogleDocsClient(store)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(client.config.TimeoutSeconds)*time.Second)
	defer cancel()
	body, err := client.readDocument(ctx, documentID)
	if err != nil {
		return err
	}
	var document struct {
		DocumentID string `json:"documentId"`
		Title      string `json:"title"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return err
	}
	fmt.Printf("Google Docs native REST test passed. Document: %s (%s)\n", document.Title, document.DocumentID)
	return nil
}
