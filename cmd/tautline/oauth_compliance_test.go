package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	testOAuthBaseURL    = "https://devspace.example"
	testOAuthCallback   = "https://chatgpt.com/connector/oauth/test-callback"
	testDynamicCallback = "http://127.0.0.1:18888/oauth/callback"
)

type oauthRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func TestOAuthDynamicRegistrationAndRefreshFlow(t *testing.T) {
	previousOwnerToken := ownerToken
	ownerToken = "test-owner-token"
	t.Cleanup(func() { ownerToken = previousOwnerToken })
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)

	oauth := newOAuth()
	registration := registerOAuthTestClient(t, oauth, testDynamicCallback)
	if registration.ClientID == "" || registration.ClientID == "chatgpt.com" {
		t.Fatalf("dynamic registration returned invalid client_id %q", registration.ClientID)
	}
	if registration.ClientIDIssuedAt <= 0 {
		t.Fatal("dynamic registration did not return client_id_issued_at")
	}
	if len(registration.RedirectURIs) != 1 || registration.RedirectURIs[0] != testDynamicCallback {
		t.Fatalf("registration did not preserve exact redirect URI: %#v", registration.RedirectURIs)
	}
	if registration.Scope != "tautline offline_access" {
		t.Fatalf("unexpected registered scope %q", registration.Scope)
	}

	verifier := "oauth-compliance-test-verifier-with-sufficient-entropy"
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	resource := testOAuthBaseURL + "/mcp"

	authorizeQuery := url.Values{
		"response_type":         {"code"},
		"client_id":             {registration.ClientID},
		"redirect_uri":          {testDynamicCallback},
		"state":                 {"state-value"},
		"scope":                 {"tautline offline_access"},
		"resource":              {resource},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authorizeRequest := httptest.NewRequest(http.MethodGet, "/authorize?"+authorizeQuery.Encode(), nil)
	authorizeResponse := httptest.NewRecorder()
	oauth.authorization(authorizeResponse, authorizeRequest)
	if authorizeResponse.Code != http.StatusOK {
		t.Fatalf("authorization page status=%d body=%s", authorizeResponse.Code, authorizeResponse.Body.String())
	}
	for _, required := range []string{
		`name="client_id" value="` + registration.ClientID + `"`,
		`name="redirect_uri" value="` + testDynamicCallback + `"`,
		`name="scope" value="tautline offline_access"`,
		`name="resource" value="` + resource + `"`,
	} {
		if !strings.Contains(authorizeResponse.Body.String(), required) {
			t.Fatalf("authorization page is missing %q", required)
		}
	}

	authorizeForm := authorizeQuery
	authorizeForm.Set("token", ownerToken)
	authorizeRequest = httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(authorizeForm.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorizeResponse = httptest.NewRecorder()
	oauth.authorizationPost(authorizeResponse, authorizeRequest)
	if authorizeResponse.Code != http.StatusFound {
		t.Fatalf("authorization approval status=%d body=%s", authorizeResponse.Code, authorizeResponse.Body.String())
	}
	redirect, err := url.Parse(authorizeResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	if code == "" || redirect.Query().Get("state") != "state-value" {
		t.Fatalf("unexpected authorization redirect %q", redirect.String())
	}

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {registration.ClientID},
		"redirect_uri":  {testDynamicCallback},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(tokenForm.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := httptest.NewRecorder()
	oauth.token(tokenResponse, tokenRequest)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token exchange status=%d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	issued := decodeOAuthTokenResponse(t, tokenResponse)
	if issued.TokenType != "Bearer" || issued.ExpiresIn != int(time.Hour.Seconds()) {
		t.Fatalf("unexpected token response: %+v", issued)
	}
	if issued.AccessToken == "" || issued.RefreshToken == "" || issued.Scope != "tautline offline_access" {
		t.Fatalf("incomplete token response: %+v", issued)
	}
	claims, valid := validateSignedToken(issued.AccessToken, "access")
	if !valid {
		t.Fatal("issued access token was rejected")
	}
	if claims.ClientID != registration.ClientID || claims.Resource != resource || claims.Scope != "tautline offline_access" {
		t.Fatalf("access token was not bound to client/resource/scope: %+v", claims)
	}

	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {issued.RefreshToken},
		"client_id":     {registration.ClientID},
		"resource":      {resource},
	}
	refreshRequest := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(refreshForm.Encode()))
	refreshRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshResponse := httptest.NewRecorder()
	oauth.token(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh exchange status=%d body=%s", refreshResponse.Code, refreshResponse.Body.String())
	}
	refreshed := decodeOAuthTokenResponse(t, refreshResponse)
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" || refreshed.RefreshToken == issued.RefreshToken {
		t.Fatal("refresh token flow did not rotate credentials")
	}
}

func TestOAuthRegistrationRejectsWildcardAndUntrustedRedirects(t *testing.T) {
	oauth := newOAuth()
	for _, redirectURI := range []string{
		"https://chatgpt.com/*",
		"https://attacker.example/callback",
		"https://chatgpt.com/callback#fragment",
	} {
		body, err := json.Marshal(oauthRegistrationRequest{
			ClientName:              "ChatGPT",
			RedirectURIs:            []string{redirectURI},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
			Scope:                   "tautline offline_access",
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body)))
		response := httptest.NewRecorder()
		oauth.register(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("redirect %q registration status=%d, want 400", redirectURI, response.Code)
		}
	}
}

func TestOAuthAuthorizationRequiresExactRegisteredRedirect(t *testing.T) {
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)
	oauth := newOAuth()
	registration := registerOAuthTestClient(t, oauth, testDynamicCallback)
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {registration.ClientID},
		"redirect_uri":          {testDynamicCallback + "/different"},
		"scope":                 {"tautline offline_access"},
		"resource":              {testOAuthBaseURL + "/mcp"},
		"code_challenge":        {"challenge"},
		"code_challenge_method": {"S256"},
	}
	request := httptest.NewRequest(http.MethodGet, "/authorize?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	oauth.authorization(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched redirect status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOAuthMetadataAdvertisesOfflineAccess(t *testing.T) {
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)
	oauth := newOAuth()
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request){
		"authorization": oauth.authorizationServerMetadata,
		"resource":      oauth.protectedResourceMetadata,
	} {
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest(http.MethodGet, "/.well-known/"+name, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s metadata status=%d", name, response.Code)
		}
		var payload struct {
			Scopes []string `json:"scopes_supported"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if !containsOAuthValue(payload.Scopes, "tautline") || !containsOAuthValue(payload.Scopes, "offline_access") {
			t.Fatalf("%s metadata scopes=%v", name, payload.Scopes)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s metadata is cacheable", name)
		}
	}
}

func TestOAuthProtectedResourceMetadataRoutes(t *testing.T) {
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)
	oauth := newOAuth()
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, oauth)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/mcp/.well-known/oauth-protected-resource",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("protected resource metadata route %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		var payload struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			Scopes               []string `json:"scopes_supported"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Resource != testOAuthBaseURL+"/mcp" {
			t.Fatalf("protected resource route %s returned resource %q", path, payload.Resource)
		}
		if len(payload.AuthorizationServers) != 1 || payload.AuthorizationServers[0] != testOAuthBaseURL {
			t.Fatalf("protected resource route %s returned authorization servers %v", path, payload.AuthorizationServers)
		}
		if !containsOAuthValue(payload.Scopes, "tautline") || !containsOAuthValue(payload.Scopes, "offline_access") {
			t.Fatalf("protected resource route %s returned scopes %v", path, payload.Scopes)
		}
	}
}

func TestOAuthVersionedProtectedResourceMetadataRoutes(t *testing.T) {
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)
	oauth := newOAuth()
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, oauth)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource/mcp/v2",
		"/mcp/v2/.well-known/oauth-protected-resource",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("versioned protected resource route %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		var payload struct {
			Resource string `json:"resource"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Resource != testOAuthBaseURL+versionedMCPPath {
			t.Fatalf("versioned protected resource route %s returned %q", path, payload.Resource)
		}
	}
}

func TestOAuthAuthorizationMetadataCompatibilityRoutes(t *testing.T) {
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)
	oauth := newOAuth()
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, oauth)

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
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("authorization metadata route %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		var payload struct {
			Issuer               string   `json:"issuer"`
			RegistrationEndpoint string   `json:"registration_endpoint"`
			Scopes               []string `json:"scopes_supported"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Issuer != testOAuthBaseURL || payload.RegistrationEndpoint != testOAuthBaseURL+"/register" {
			t.Fatalf("authorization metadata route %s returned issuer=%q registration=%q", path, payload.Issuer, payload.RegistrationEndpoint)
		}
		if !containsOAuthValue(payload.Scopes, "tautline") || !containsOAuthValue(payload.Scopes, "offline_access") {
			t.Fatalf("authorization metadata route %s returned scopes %v", path, payload.Scopes)
		}
	}
}

func TestOAuthChallengeUsesEndpointSpecificProtectedResourceMetadata(t *testing.T) {
	t.Setenv("TAUTLINE_PUBLIC_BASE_URL", testOAuthBaseURL)
	t.Setenv("TAUTLINE_REQUIRE_AUTH", "true")
	oauth := newOAuth()
	handler := oauth.requireBearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unauthenticated request reached the protected MCP handler")
	}))

	for _, testCase := range []struct {
		path         string
		metadataPath string
	}{
		{path: canonicalMCPPath, metadataPath: "/.well-known/oauth-protected-resource/mcp"},
		{path: versionedMCPPath, metadataPath: "/.well-known/oauth-protected-resource/mcp/v2"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, testCase.path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated MCP %s status=%d body=%s", testCase.path, response.Code, response.Body.String())
		}
		expected := `Bearer resource_metadata="` + testOAuthBaseURL + testCase.metadataPath + `", scope="tautline"`
		if challenge := response.Header().Get("WWW-Authenticate"); challenge != expected {
			t.Fatalf("%s WWW-Authenticate=%q, want %q", testCase.path, challenge, expected)
		}
		if response.Header().Get("Vary") != "Authorization" {
			t.Fatalf("%s challenge does not vary on Authorization", testCase.path)
		}
	}
}

func TestOAuthChatGPTRegistrationUsesV24Compatibility(t *testing.T) {
	oauth := newOAuth()
	for _, request := range []oauthRegistrationRequest{
		{RedirectURIs: []string{testOAuthCallback}},
		{RedirectURI: testOAuthCallback},
		{},
	} {
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		oauth.register(response, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body))))
		if response.Code != http.StatusCreated {
			t.Fatalf("ChatGPT compatibility registration status=%d body=%s", response.Code, response.Body.String())
		}
		var registration oauthRegistrationResponse
		if err := json.Unmarshal(response.Body.Bytes(), &registration); err != nil {
			t.Fatal(err)
		}
		if registration.ClientID != "chatgpt.com" || registration.Scope != "tautline offline_access" {
			t.Fatalf("unexpected ChatGPT compatibility registration: %+v", registration)
		}
		if len(registration.RedirectURIs) == 0 {
			t.Fatal("ChatGPT compatibility registration returned no redirect URI")
		}
	}
}

func TestOAuthLegacyChatGPTTokenExchangeAllowsOmittedClientFields(t *testing.T) {
	oauth := newOAuth()
	verifier := "legacy-chatgpt-token-exchange-verifier-with-sufficient-entropy"
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	resource := testOAuthBaseURL + versionedMCPPath
	code := "legacy-chatgpt-authorization-code"
	oauth.codes[code] = authorizationCode{
		ClientID:            "chatgpt.com",
		RedirectURI:         testOAuthCallback,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Scope:               "tautline offline_access",
		Resource:            resource,
		ExpiresAt:           time.Now().Add(time.Minute),
	}

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(tokenForm.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := httptest.NewRecorder()
	oauth.token(tokenResponse, tokenRequest)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("legacy ChatGPT token exchange status=%d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	issued := decodeOAuthTokenResponse(t, tokenResponse)

	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {issued.RefreshToken},
		"resource":      {resource},
	}
	refreshRequest := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(refreshForm.Encode()))
	refreshRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshResponse := httptest.NewRecorder()
	oauth.token(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("legacy ChatGPT refresh exchange status=%d body=%s", refreshResponse.Code, refreshResponse.Body.String())
	}
}

func TestOAuthVersionedResourceNormalization(t *testing.T) {
	for _, testCase := range []struct {
		raw  string
		want string
	}{
		{raw: "", want: testOAuthBaseURL + canonicalMCPPath},
		{raw: testOAuthBaseURL + canonicalMCPPath, want: testOAuthBaseURL + canonicalMCPPath},
		{raw: testOAuthBaseURL + versionedMCPPath, want: testOAuthBaseURL + versionedMCPPath},
	} {
		got, err := normalizeOAuthMCPResource(testCase.raw, testOAuthBaseURL)
		if err != nil {
			t.Fatalf("normalize resource %q: %v", testCase.raw, err)
		}
		if got != testCase.want {
			t.Fatalf("normalize resource %q=%q, want %q", testCase.raw, got, testCase.want)
		}
	}
	for _, raw := range []string{
		"https://attacker.example/mcp",
		testOAuthBaseURL + "/other",
		testOAuthBaseURL + versionedMCPPath + "?query=1",
	} {
		if _, err := normalizeOAuthMCPResource(raw, testOAuthBaseURL); err == nil {
			t.Fatalf("invalid MCP resource %q was accepted", raw)
		}
	}
}

func TestOAuthLegacyClientRemainsCompatible(t *testing.T) {
	oauth := newOAuth()
	if !oauth.clientAllowsRedirect("chatgpt.com", testOAuthCallback) {
		t.Fatal("legacy ChatGPT client callback was rejected")
	}
	if oauth.clientAllowsRedirect("chatgpt.com", "https://attacker.example/callback") {
		t.Fatal("legacy ChatGPT client accepted an untrusted callback")
	}
	legacy := issueSignedToken("access", "tautline", time.Minute)
	claims, valid := validateSignedToken(legacy, "access")
	if !valid || claims.Resource != "" || claims.ClientID != "" {
		t.Fatalf("legacy token compatibility failed: valid=%v claims=%+v", valid, claims)
	}
}

func TestOAuthDynamicClientPersistsAcrossRestart(t *testing.T) {
	runtimeDir := t.TempDir()
	first := newOAuth(runtimeDir)
	registration := registerOAuthTestClient(t, first, testDynamicCallback)
	stateInfo, err := os.Stat(filepath.Join(runtimeDir, "state", "oauth-clients.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stateInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("OAuth client registry permissions are too broad: %o", stateInfo.Mode().Perm())
	}
	second := newOAuth(runtimeDir)
	if !second.clientAllowsRedirect(registration.ClientID, testDynamicCallback) {
		t.Fatal("persisted dynamic client was not restored after restart")
	}
	if second.clientAllowsRedirect(registration.ClientID, testDynamicCallback+"/different") {
		t.Fatal("restored dynamic client accepted a non-registered redirect URI")
	}
}

func registerOAuthTestClient(t *testing.T, oauth *oauthServer, redirectURI string) oauthRegistrationResponse {
	t.Helper()
	body, err := json.Marshal(oauthRegistrationRequest{
		ClientName:              "ChatGPT Tautline Test",
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		Scope:                   "tautline offline_access",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	oauth.register(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", response.Code, response.Body.String())
	}
	var registration oauthRegistrationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	return registration
}

func decodeOAuthTokenResponse(t *testing.T, response *httptest.ResponseRecorder) oauthTokenResponse {
	t.Helper()
	var token oauthTokenResponse
	if err := json.Unmarshal(response.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	return token
}
