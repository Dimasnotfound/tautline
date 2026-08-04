package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	authCodeTTL     = 5 * time.Minute

	canonicalMCPPath = "/mcp"
	versionedMCPPath = "/mcp/v2"
)

var (
	allowedRoots        []string
	ownerToken          string
	ownerTokenGenerated bool
)

func loadConfig() {
	ownerToken = firstEnvironment("TAUTLINE_OWNER_TOKEN", "DEVSPACE_OWNER_TOKEN")
	ownerTokenGenerated = ownerToken == ""
	if ownerTokenGenerated {
		ownerToken = randToken()
		fmt.Println("warning: TAUTLINE_OWNER_TOKEN was not set; a temporary token was generated for this process")
	}

	roots := firstEnvironment("TAUTLINE_ALLOWED_ROOTS", "DEVSPACE_ALLOWED_ROOTS")
	if roots == "" {
		roots = "."
	}
	allowedRoots = allowedRoots[:0]
	for _, root := range strings.Split(roots, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		canonical, err := canonicalPath(root)
		if err != nil {
			continue
		}
		allowedRoots = append(allowedRoots, canonical)
	}
	if len(allowedRoots) == 0 {
		panic("TAUTLINE_ALLOWED_ROOTS contains no valid roots")
	}
}

func randToken() string {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(absolute)
	if evaluated, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(evaluated), nil
	}

	ancestor := clean
	var suffix []string
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			evaluated, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				evaluated = filepath.Join(evaluated, suffix[index])
			}
			return filepath.Clean(evaluated), nil
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return clean, nil
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
}

func resolvePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	clean, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	for _, root := range allowedRoots {
		relative, err := filepath.Rel(root, clean)
		if err != nil {
			continue
		}
		if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("path %q is outside the allowed roots", path)
}

type authorizationCode struct {
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	Resource            string
	ExpiresAt           time.Time
}

type signedTokenClaims struct {
	Type     string `json:"typ"`
	Scope    string `json:"scope"`
	ClientID string `json:"client_id,omitempty"`
	Resource string `json:"aud,omitempty"`
	Exp      int64  `json:"exp"`
	Nonce    string `json:"nonce"`
}

type oauthClient struct {
	ClientID                string
	ClientName              string
	RedirectURIs            map[string]struct{}
	GrantTypes              []string
	ResponseTypes           []string
	TokenEndpointAuthMethod string
	Scope                   string
	Legacy                  bool
}

type oauthRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	RedirectURI             string   `json:"redirect_uri"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type persistedOAuthClient struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type oauthServer struct {
	mu        sync.Mutex
	codes     map[string]authorizationCode
	clients   map[string]oauthClient
	statePath string
}

func newOAuth(runtimeDirs ...string) *oauthServer {
	oauth := &oauthServer{
		codes: make(map[string]authorizationCode),
		clients: map[string]oauthClient{
			"chatgpt.com": {
				ClientID:                "chatgpt.com",
				ClientName:              "ChatGPT (legacy)",
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				ResponseTypes:           []string{"code"},
				TokenEndpointAuthMethod: "none",
				Scope:                   "tautline offline_access",
				Legacy:                  true,
			},
		},
	}
	if len(runtimeDirs) > 0 && strings.TrimSpace(runtimeDirs[0]) != "" {
		oauth.statePath = filepath.Join(runtimeDirs[0], "state", "oauth-clients.json")
		if err := oauth.loadClients(); err != nil {
			fmt.Fprintln(os.Stderr, "OAuth client registry:", err, "Continuing with legacy compatibility only.")
		}
	}
	return oauth
}

func (o *oauthServer) loadClients() error {
	data, err := os.ReadFile(o.statePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", o.statePath, err)
	}
	var persisted []persistedOAuthClient
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("decode %s: %w", o.statePath, err)
	}
	for _, saved := range persisted {
		if !strings.HasPrefix(saved.ClientID, "tautline-") {
			return fmt.Errorf("stored client_id %q is invalid", saved.ClientID)
		}
		client, err := validateOAuthRegistration(oauthRegistrationRequest{
			ClientName:              saved.ClientName,
			RedirectURIs:            saved.RedirectURIs,
			GrantTypes:              saved.GrantTypes,
			ResponseTypes:           saved.ResponseTypes,
			TokenEndpointAuthMethod: saved.TokenEndpointAuthMethod,
			Scope:                   saved.Scope,
		})
		if err != nil {
			return fmt.Errorf("stored client %q: %w", saved.ClientID, err)
		}
		client.ClientID = saved.ClientID
		o.clients[client.ClientID] = client
	}
	return nil
}

func (o *oauthServer) persistClientsLocked() error {
	if o.statePath == "" {
		return nil
	}
	clients := make([]persistedOAuthClient, 0, len(o.clients))
	for _, client := range o.clients {
		if client.Legacy {
			continue
		}
		redirectURIs := make([]string, 0, len(client.RedirectURIs))
		for redirectURI := range client.RedirectURIs {
			redirectURIs = append(redirectURIs, redirectURI)
		}
		sort.Strings(redirectURIs)
		clients = append(clients, persistedOAuthClient{
			ClientID:                client.ClientID,
			ClientName:              client.ClientName,
			RedirectURIs:            redirectURIs,
			GrantTypes:              append([]string(nil), client.GrantTypes...),
			ResponseTypes:           append([]string(nil), client.ResponseTypes...),
			TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
			Scope:                   client.Scope,
		})
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].ClientID < clients[j].ClientID })
	data, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return replaceOAuthStateFile(o.statePath, data)
}

func replaceOAuthStateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".oauth-clients-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
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
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace OAuth client registry: %w", err)
	}
	return nil
}

func (o *oauthServer) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := baseURL(r)
	writeJSONWithNoStore(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"registration_endpoint":                 base + "/register",
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"scopes_supported":                      []string{"tautline", "offline_access"},
		"resource_indicators_supported":         true,
	})
}

func (o *oauthServer) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	o.protectedResourceMetadataFor(canonicalMCPPath)(w, r)
}

func (o *oauthServer) protectedResourceMetadataFor(resourcePath string) http.HandlerFunc {
	resourcePath = normalizeMCPPath(resourcePath)
	return func(w http.ResponseWriter, r *http.Request) {
		base := baseURL(r)
		writeJSONWithNoStore(w, http.StatusOK, map[string]any{
			"resource":                 base + resourcePath,
			"authorization_servers":    []string{base},
			"scopes_supported":         []string{"tautline", "offline_access"},
			"bearer_methods_supported": []string{"header"},
			"resource_name":            "Tautline",
		})
	}
}

func registerOAuthRoutes(mux *http.ServeMux, oauth *oauthServer) {
	canonicalResource := oauth.protectedResourceMetadataFor(canonicalMCPPath)
	versionedResource := oauth.protectedResourceMetadataFor(versionedMCPPath)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/mcp/.well-known/oauth-protected-resource",
	} {
		mux.HandleFunc(path, canonicalResource)
	}
	for _, path := range []string{
		"/.well-known/oauth-protected-resource/mcp/v2",
		"/mcp/v2/.well-known/oauth-protected-resource",
	} {
		mux.HandleFunc(path, versionedResource)
	}

	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-authorization-server/mcp",
		"/.well-known/oauth-authorization-server/mcp/v2",
		"/mcp/.well-known/oauth-authorization-server",
		"/mcp/v2/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
		"/.well-known/openid-configuration/mcp",
		"/.well-known/openid-configuration/mcp/v2",
		"/mcp/.well-known/openid-configuration",
		"/mcp/v2/.well-known/openid-configuration",
	} {
		mux.HandleFunc(path, oauth.authorizationServerMetadata)
	}

	mux.HandleFunc("/register", oauth.register)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			oauth.authorizationPost(w, r)
			return
		}
		oauth.authorization(w, r)
	})
	mux.HandleFunc("/token", oauth.token)
}

func (o *oauthServer) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request oauthRegistrationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := decoder.Decode(&request); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "registration body must be valid JSON")
		return
	}
	if len(request.RedirectURIs) == 0 && strings.TrimSpace(request.RedirectURI) != "" {
		request.RedirectURIs = []string{strings.TrimSpace(request.RedirectURI)}
	}
	if shouldUseLegacyChatGPTRegistration(request) {
		writeJSONWithNoStore(w, http.StatusCreated, map[string]any{
			"client_id":                  "chatgpt.com",
			"client_name":                "ChatGPT",
			"redirect_uris":              chatGPTRegistrationRedirects(request.RedirectURIs),
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"token_endpoint_auth_method": "none",
			"scope":                      "tautline offline_access",
		})
		return
	}
	client, err := validateOAuthRegistration(request)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	client.ClientID = "tautline-" + randToken()

	o.mu.Lock()
	o.clients[client.ClientID] = client
	if err := o.persistClientsLocked(); err != nil {
		delete(o.clients, client.ClientID)
		o.mu.Unlock()
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not persist OAuth client registration")
		return
	}
	o.mu.Unlock()

	redirectURIs := make([]string, 0, len(client.RedirectURIs))
	for redirectURI := range client.RedirectURIs {
		redirectURIs = append(redirectURIs, redirectURI)
	}
	sort.Strings(redirectURIs)
	writeJSONWithNoStore(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              redirectURIs,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"scope":                      client.Scope,
	})
}

var approvalTemplate = template.Must(template.New("approval").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Approve Tautline</title>
<style>
:root{color-scheme:light dark;font-family:system-ui,sans-serif}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f4f4f5;color:#18181b}.card{width:min(420px,calc(100% - 32px));padding:24px;border:1px solid #d4d4d8;border-radius:14px;background:#fff;box-shadow:0 12px 32px #00000012}h1{font-size:20px;margin:0 0 8px}p{color:#71717a;line-height:1.5}label{display:block;font-weight:600;margin:18px 0 6px}input{width:100%;padding:11px 12px;border:1px solid #a1a1aa;border-radius:9px;font:inherit;box-sizing:border-box}button{width:100%;margin-top:14px;padding:11px;border:0;border-radius:9px;background:#18181b;color:#fff;font:inherit;font-weight:700;cursor:pointer}@media(prefers-color-scheme:dark){body{background:#111;color:#fafafa}.card{background:#1c1c1c;border-color:#3f3f46}p{color:#a1a1aa}button{background:#fafafa;color:#18181b}}</style>
</head>
<body><main class="card"><h1>Connect ChatGPT to Tautline</h1><p>This grants access only to the configured local project roots and Tautline tools.</p>
<form method="post" action="/authorize">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<input type="hidden" name="resource" value="{{.Resource}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
<label for="token">Owner token</label><input id="token" type="password" name="token" autocomplete="current-password" required autofocus>
<button type="submit">Approve connection</button>
</form></main></body></html>`))

func (o *oauthServer) authorization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	if query.Get("response_type") != "code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "response_type must be code")
		return
	}
	clientID := strings.TrimSpace(query.Get("client_id"))
	redirectURI := strings.TrimSpace(query.Get("redirect_uri"))
	if !o.clientAllowsRedirect(clientID, redirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id or redirect_uri is not registered")
		return
	}
	challenge := query.Get("code_challenge")
	if challenge == "" || query.Get("code_challenge_method") != "S256" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "PKCE S256 is required")
		return
	}
	scope, err := normalizeOAuthScope(query.Get("scope"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", err.Error())
		return
	}
	resource, err := normalizeOAuthMCPResource(query.Get("resource"), baseURL(r))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = approvalTemplate.Execute(w, map[string]string{
		"ClientID":            clientID,
		"RedirectURI":         redirectURI,
		"State":               query.Get("state"),
		"Scope":               scope,
		"Resource":            resource,
		"CodeChallenge":       challenge,
		"CodeChallengeMethod": "S256",
	})
}

func (o *oauthServer) authorizationPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	redirectURI := strings.TrimSpace(r.FormValue("redirect_uri"))
	if !o.clientAllowsRedirect(clientID, redirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id or redirect_uri is not registered")
		return
	}
	if !hmac.Equal([]byte(r.FormValue("token")), []byte(ownerToken)) {
		http.Error(w, "invalid owner token", http.StatusUnauthorized)
		return
	}
	challenge := r.FormValue("code_challenge")
	if challenge == "" || r.FormValue("code_challenge_method") != "S256" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "PKCE S256 is required")
		return
	}
	scope, err := normalizeOAuthScope(r.FormValue("scope"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", err.Error())
		return
	}
	resource, err := normalizeOAuthMCPResource(r.FormValue("resource"), baseURL(r))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}
	code := randToken()
	o.mu.Lock()
	o.codes[code] = authorizationCode{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Scope:               scope,
		Resource:            resource,
		ExpiresAt:           time.Now().Add(authCodeTTL),
	}
	o.mu.Unlock()

	redirect, _ := url.Parse(redirectURI)
	values := redirect.Query()
	values.Set("code", code)
	if state := r.FormValue("state"); state != "" {
		values.Set("state", state)
	}
	redirect.RawQuery = values.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (o *oauthServer) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}

	switch r.FormValue("grant_type") {
	case "authorization_code":
		o.exchangeAuthorizationCode(w, r)
	case "refresh_token":
		o.exchangeRefreshToken(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "supported grants are authorization_code and refresh_token")
	}
}

func (o *oauthServer) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	codeValue := r.FormValue("code")
	o.mu.Lock()
	code, found := o.codes[codeValue]
	if found {
		delete(o.codes, codeValue)
	}
	o.mu.Unlock()
	if !found || time.Now().After(code.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	redirectURI := strings.TrimSpace(r.FormValue("redirect_uri"))
	if redirectURI == "" {
		if !isLegacyChatGPTClient(code.ClientID) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri is required")
			return
		}
	} else if redirectURI != code.RedirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match")
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if clientID == "" {
		if !isLegacyChatGPTClient(code.ClientID) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id is required")
			return
		}
	} else if clientID != code.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match")
		return
	}
	if !verifyPKCE(r.FormValue("code_verifier"), code.CodeChallenge) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	resource, err := normalizeOAuthResource(r.FormValue("resource"), code.Resource)
	if err != nil || resource != code.Resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the authorization request")
		return
	}
	writeTokenResponse(w, code.Scope, code.ClientID, code.Resource)
}

func (o *oauthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	claims, valid := validateSignedToken(r.FormValue("refresh_token"), "refresh")
	if !valid {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if clientID == "" {
		if claims.ClientID != "" && !isLegacyChatGPTClient(claims.ClientID) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id is required")
			return
		}
	} else if claims.ClientID != "" && clientID != claims.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match the refresh token")
		return
	}
	if requested := strings.TrimSpace(r.FormValue("resource")); requested != "" && claims.Resource != "" {
		resource, err := normalizeOAuthResource(requested, claims.Resource)
		if err != nil || resource != claims.Resource {
			writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the refresh token")
			return
		}
	}
	writeTokenResponse(w, claims.Scope, claims.ClientID, claims.Resource)
}

func writeTokenResponse(w http.ResponseWriter, scope, clientID, resource string) {
	writeJSONWithNoStore(w, http.StatusOK, map[string]any{
		"access_token":  issueSignedTokenFor("access", scope, clientID, resource, accessTokenTTL),
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"refresh_token": issueSignedTokenFor("refresh", scope, clientID, resource, refreshTokenTTL),
		"scope":         scope,
	})
}

func (o *oauthServer) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth := firstEnvironment("TAUTLINE_REQUIRE_AUTH", "DEVSPACE_REQUIRE_AUTH")
		if strings.EqualFold(requireAuth, "false") || requireAuth == "0" {
			next.ServeHTTP(w, r)
			return
		}
		resourcePath := normalizeMCPPath(r.URL.Path)
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
			if claims, valid := validateSignedToken(strings.TrimSpace(authorization[7:]), "access"); valid {
				expectedResource := baseURL(r) + resourcePath
				if claims.Resource == "" || claims.Resource == expectedResource {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		metadataURL := baseURL(r) + protectedResourceMetadataPath(resourcePath)
		w.Header().Set("Vary", "Authorization")
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadataURL+`", scope="tautline"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_token", "a valid Tautline bearer token is required")
	})
}

func issueSignedToken(tokenType, scope string, ttl time.Duration) string {
	return issueSignedTokenFor(tokenType, scope, "", "", ttl)
}

func issueSignedTokenFor(tokenType, scope, clientID, resource string, ttl time.Duration) string {
	claims := signedTokenClaims{
		Type:     tokenType,
		Scope:    defaultScope(scope),
		ClientID: clientID,
		Resource: resource,
		Exp:      time.Now().Add(ttl).Unix(),
		Nonce:    randToken(),
	}
	payload, _ := json.Marshal(claims)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := signTokenPayload(encodedPayload)
	return encodedPayload + "." + signature
}

func validateSignedToken(token, expectedType string) (signedTokenClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return signedTokenClaims{}, false
	}
	expectedSignature := signTokenPayload(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSignature)) {
		return signedTokenClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return signedTokenClaims{}, false
	}
	var claims signedTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return signedTokenClaims{}, false
	}
	if claims.Type != expectedType || claims.Exp <= time.Now().Unix() || !oauthScopeContains(claims.Scope, "tautline") {
		return signedTokenClaims{}, false
	}
	return claims, true
}

func signTokenPayload(payload string) string {
	mac := hmac.New(sha256.New, []byte(ownerToken))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	hash := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(hash[:])
	return hmac.Equal([]byte(computed), []byte(challenge))
}

func validateOAuthRegistration(request oauthRegistrationRequest) (oauthClient, error) {
	if len(request.RedirectURIs) == 0 || len(request.RedirectURIs) > 10 {
		return oauthClient{}, fmt.Errorf("redirect_uris must contain between 1 and 10 exact callback URLs")
	}
	redirectURIs := make(map[string]struct{}, len(request.RedirectURIs))
	for _, redirectURI := range request.RedirectURIs {
		redirectURI = strings.TrimSpace(redirectURI)
		if !allowedRedirectURI(redirectURI) {
			return oauthClient{}, fmt.Errorf("redirect_uri is not an allowed ChatGPT, OpenAI, or loopback callback")
		}
		redirectURIs[redirectURI] = struct{}{}
	}

	grantTypes := request.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	if !oauthValuesAllowed(grantTypes, "authorization_code", "refresh_token") || !containsOAuthValue(grantTypes, "authorization_code") {
		return oauthClient{}, fmt.Errorf("grant_types must include authorization_code and may include refresh_token")
	}
	responseTypes := request.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	if !oauthValuesAllowed(responseTypes, "code") || !containsOAuthValue(responseTypes, "code") {
		return oauthClient{}, fmt.Errorf("response_types must contain only code")
	}
	authMethod := strings.TrimSpace(request.TokenEndpointAuthMethod)
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" {
		return oauthClient{}, fmt.Errorf("token_endpoint_auth_method must be none for the public ChatGPT client")
	}
	scope, err := normalizeOAuthScope(request.Scope)
	if err != nil {
		return oauthClient{}, err
	}
	clientName := strings.TrimSpace(request.ClientName)
	if clientName == "" {
		clientName = "ChatGPT"
	}
	return oauthClient{
		ClientName:              clientName,
		RedirectURIs:            redirectURIs,
		GrantTypes:              append([]string(nil), grantTypes...),
		ResponseTypes:           append([]string(nil), responseTypes...),
		TokenEndpointAuthMethod: authMethod,
		Scope:                   scope,
	}, nil
}

func (o *oauthServer) clientAllowsRedirect(clientID, redirectURI string) bool {
	if clientID == "" || redirectURI == "" {
		return false
	}
	o.mu.Lock()
	client, found := o.clients[clientID]
	o.mu.Unlock()
	if !found {
		return false
	}
	if client.Legacy {
		return allowedRedirectURI(redirectURI)
	}
	_, found = client.RedirectURIs[redirectURI]
	return found
}

func allowedRedirectURI(raw string) bool {
	if raw == "" || strings.Contains(raw, "*") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "https" {
		return host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com") || host == "openai.com" || strings.HasSuffix(host, ".openai.com")
	}
	return scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func isLegacyChatGPTClient(clientID string) bool {
	return strings.EqualFold(strings.TrimSpace(clientID), "chatgpt.com")
}

func shouldUseLegacyChatGPTRegistration(request oauthRegistrationRequest) bool {
	if len(request.RedirectURIs) == 0 {
		return true
	}
	for _, redirectURI := range request.RedirectURIs {
		if !isChatGPTRedirectURI(redirectURI) {
			return false
		}
	}
	return true
}

func chatGPTRegistrationRedirects(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	redirects := make([]string, 0, len(values))
	for _, raw := range values {
		redirectURI := strings.TrimSpace(raw)
		if !isChatGPTRedirectURI(redirectURI) {
			continue
		}
		if _, exists := seen[redirectURI]; exists {
			continue
		}
		seen[redirectURI] = struct{}{}
		redirects = append(redirects, redirectURI)
	}
	if len(redirects) == 0 {
		return []string{"https://chatgpt.com/*"}
	}
	sort.Strings(redirects)
	return redirects
}

func isChatGPTRedirectURI(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "*") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com") || host == "openai.com" || strings.HasSuffix(host, ".openai.com")
}

func normalizeMCPPath(raw string) string {
	path := "/" + strings.Trim(strings.TrimSpace(raw), "/")
	if path == versionedMCPPath {
		return versionedMCPPath
	}
	return canonicalMCPPath
}

func protectedResourceMetadataPath(resourcePath string) string {
	if normalizeMCPPath(resourcePath) == versionedMCPPath {
		return "/.well-known/oauth-protected-resource/mcp/v2"
	}
	return "/.well-known/oauth-protected-resource/mcp"
}

func normalizeOAuthMCPResource(raw, base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" || baseURL.User != nil || baseURL.Fragment != "" {
		return "", fmt.Errorf("OAuth public base URL is invalid")
	}
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return base + canonicalMCPPath, nil
	}
	candidate = strings.TrimRight(candidate, "/")
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", fmt.Errorf("resource must be an absolute MCP URL without query or fragment")
	}
	if !strings.EqualFold(parsed.Scheme, baseURL.Scheme) || !strings.EqualFold(parsed.Host, baseURL.Host) {
		return "", fmt.Errorf("resource must use the configured public origin")
	}
	if parsed.Path != canonicalMCPPath && parsed.Path != versionedMCPPath {
		return "", fmt.Errorf("resource must target %s or %s", canonicalMCPPath, versionedMCPPath)
	}
	return base + parsed.Path, nil
}

func normalizeOAuthScope(raw string) (string, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		fields = []string{"tautline", "offline_access"}
	}
	seen := make(map[string]bool, len(fields))
	for _, scope := range fields {
		if scope != "tautline" && scope != "offline_access" {
			return "", fmt.Errorf("unsupported scope %q", scope)
		}
		seen[scope] = true
	}
	if !seen["tautline"] {
		return "", fmt.Errorf("scope tautline is required")
	}
	normalized := []string{"tautline"}
	if seen["offline_access"] {
		normalized = append(normalized, "offline_access")
	}
	return strings.Join(normalized, " "), nil
}

func normalizeOAuthResource(raw, expected string) (string, error) {
	expected = strings.TrimRight(strings.TrimSpace(expected), "/")
	if expected == "" {
		return "", fmt.Errorf("OAuth resource is not configured")
	}
	if strings.TrimSpace(raw) == "" {
		return expected, nil
	}
	candidate := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("resource must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("resource must use HTTP or HTTPS")
	}
	if candidate != expected {
		return "", fmt.Errorf("resource must match %s", expected)
	}
	return expected, nil
}

func oauthScopeContains(scope, expected string) bool {
	for _, value := range strings.Fields(scope) {
		if value == expected {
			return true
		}
	}
	return false
}

func oauthValuesAllowed(values []string, allowed ...string) bool {
	for _, value := range values {
		if !containsOAuthValue(allowed, value) {
			return false
		}
	}
	return true
}

func containsOAuthValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func defaultScope(raw string) string {
	scope, err := normalizeOAuthScope(raw)
	if err != nil {
		return "tautline"
	}
	return scope
}

func baseURL(r *http.Request) string {
	if public := firstEnvironment("TAUTLINE_PUBLIC_BASE_URL"); public != "" {
		return strings.TrimRight(public, "/")
	}
	if runtime, err := currentApplicationRuntime(); err == nil {
		if public := strings.TrimSpace(runtime.tunnel.status().PublicURL); public != "" {
			return strings.TrimRight(public, "/")
		}
		if public := strings.TrimSpace(runtime.config.snapshot().PublicBaseURL); public != "" {
			return strings.TrimRight(public, "/")
		}
	}
	if public := firstEnvironment("DEVSPACE_PUBLIC_BASE_URL"); public != "" {
		return strings.TrimRight(public, "/")
	}
	scheme := "http"
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONWithNoStore(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, payload)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSONWithNoStore(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}
