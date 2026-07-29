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
	"strings"
	"sync"
	"time"
)

const (
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	authCodeTTL     = 5 * time.Minute
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
	ExpiresAt           time.Time
}

type signedTokenClaims struct {
	Type  string `json:"typ"`
	Scope string `json:"scope"`
	Exp   int64  `json:"exp"`
	Nonce string `json:"nonce"`
}

type oauthServer struct {
	mu    sync.Mutex
	codes map[string]authorizationCode
}

func newOAuth() *oauthServer {
	return &oauthServer{codes: make(map[string]authorizationCode)}
}

func (o *oauthServer) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"registration_endpoint":                 base + "/register",
		"response_types_supported":              []string{"code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"scopes_supported":                      []string{"tautline"},
	})
}

func (o *oauthServer) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"scopes_supported":         []string{"tautline"},
		"bearer_methods_supported": []string{"header"},
	})
}

func (o *oauthServer) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  "chatgpt.com",
		"client_name":                "ChatGPT",
		"redirect_uris":              []string{"https://chatgpt.com/*"},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      "tautline",
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
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
	redirectURI := query.Get("redirect_uri")
	if !allowedRedirectURI(redirectURI) {
		http.Error(w, "redirect_uri is not allowed", http.StatusBadRequest)
		return
	}
	challenge := query.Get("code_challenge")
	if challenge == "" || query.Get("code_challenge_method") != "S256" {
		http.Error(w, "PKCE S256 is required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = approvalTemplate.Execute(w, map[string]string{
		"ClientID":            query.Get("client_id"),
		"RedirectURI":         redirectURI,
		"State":               query.Get("state"),
		"Scope":               defaultScope(query.Get("scope")),
		"CodeChallenge":       challenge,
		"CodeChallengeMethod": "S256",
	})
}

func (o *oauthServer) authorizationPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	redirectURI := r.FormValue("redirect_uri")
	if !allowedRedirectURI(redirectURI) {
		http.Error(w, "redirect_uri is not allowed", http.StatusBadRequest)
		return
	}
	if !hmac.Equal([]byte(r.FormValue("token")), []byte(ownerToken)) {
		http.Error(w, "invalid owner token", http.StatusUnauthorized)
		return
	}
	challenge := r.FormValue("code_challenge")
	if challenge == "" || r.FormValue("code_challenge_method") != "S256" {
		http.Error(w, "PKCE S256 is required", http.StatusBadRequest)
		return
	}
	code := randToken()
	o.mu.Lock()
	o.codes[code] = authorizationCode{
		ClientID:            r.FormValue("client_id"),
		RedirectURI:         redirectURI,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Scope:               defaultScope(r.FormValue("scope")),
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
	if redirectURI := r.FormValue("redirect_uri"); redirectURI != "" && redirectURI != code.RedirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match")
		return
	}
	if clientID := r.FormValue("client_id"); clientID != "" && code.ClientID != "" && clientID != code.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match")
		return
	}
	if !verifyPKCE(r.FormValue("code_verifier"), code.CodeChallenge) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	writeTokenResponse(w, code.Scope)
}

func (o *oauthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	claims, valid := validateSignedToken(r.FormValue("refresh_token"), "refresh")
	if !valid {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	writeTokenResponse(w, claims.Scope)
}

func writeTokenResponse(w http.ResponseWriter, scope string) {
	writeJSONWithNoStore(w, http.StatusOK, map[string]any{
		"access_token":  issueSignedToken("access", scope, accessTokenTTL),
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"refresh_token": issueSignedToken("refresh", scope, refreshTokenTTL),
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
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
			if _, valid := validateSignedToken(strings.TrimSpace(authorization[7:]), "access"); valid {
				next.ServeHTTP(w, r)
				return
			}
		}
		metadataURL := baseURL(r) + "/.well-known/oauth-protected-resource/mcp"
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadataURL+`", scope="tautline"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_token", "a valid Tautline bearer token is required")
	})
}

func issueSignedToken(tokenType, scope string, ttl time.Duration) string {
	claims := signedTokenClaims{
		Type:  tokenType,
		Scope: defaultScope(scope),
		Exp:   time.Now().Add(ttl).Unix(),
		Nonce: randToken(),
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
	if claims.Type != expectedType || claims.Exp <= time.Now().Unix() || claims.Scope != "tautline" {
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

func allowedRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "https" {
		return host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com") || host == "openai.com" || strings.HasSuffix(host, ".openai.com")
	}
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1")
}

func defaultScope(_ string) string {
	return "tautline"
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
