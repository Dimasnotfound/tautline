package main

import (
	"context"
	"crypto/hmac"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

//go:embed web/index.html web/app.css web/app.js web/icon.svg
var dashboardAssets embed.FS

const dashboardCookieName = "tautline_admin"

type dashboardConfigView struct {
	Port          string           `json:"port"`
	RuntimeDir    string           `json:"runtime_dir"`
	WorktreeRoot  string           `json:"worktree_root"`
	PublicBaseURL string           `json:"public_base_url,omitempty"`
	WidgetDomain  string           `json:"widget_domain,omitempty"`
	AgentEnabled  bool             `json:"agent_enabled"`
	AgentBackend  string           `json:"agent_backend"`
	Router        RouterConfigView `json:"router"`
	Lightpanda    LightpandaConfig `json:"lightpanda"`
	Tunnel        TunnelConfig     `json:"tunnel"`
}

type RouterConfigView struct {
	BaseURL       string   `json:"base_url"`
	APIKey        string   `json:"api_key"`
	DefaultModel  string   `json:"default_model"`
	AllowedModels []string `json:"allowed_models"`
}

type dashboardState struct {
	Service      string              `json:"service"`
	Version      string              `json:"version"`
	StartedAt    time.Time           `json:"started_at"`
	Uptime       int64               `json:"uptime_seconds"`
	CSRF         string              `json:"csrf"`
	LocalURL     string              `json:"local_url"`
	MCPLocalURL  string              `json:"mcp_local_url"`
	MCPPublicURL string              `json:"mcp_public_url,omitempty"`
	OwnerToken   string              `json:"owner_token"`
	AllowedRoots []string            `json:"allowed_roots"`
	Config       dashboardConfigView `json:"config"`
	Router       RouterStatus        `json:"router"`
	RelayBridge  relayBridgeStatus   `json:"relay_bridge"`
	Lightpanda   LightpandaStatus    `json:"lightpanda"`
	Tunnel       TunnelStatus        `json:"tunnel"`
	MCPServers   []ExternalMCPStatus `json:"mcp_servers"`
	Slots        []AgentSlot         `json:"slots"`
	Runs         []AgentRun          `json:"runs"`
}

type settingsUpdate struct {
	RouterBaseURL       *string   `json:"router_base_url"`
	RouterAPIKey        *string   `json:"router_api_key"`
	RouterDefaultModel  *string   `json:"router_default_model"`
	RouterAllowedModels *[]string `json:"router_allowed_models"`
	AgentEnabled        *bool     `json:"agent_enabled"`
	AgentBackend        *string   `json:"agent_backend"`
	LightpandaPath      *string   `json:"lightpanda_path"`
	LightpandaPort      *int      `json:"lightpanda_port"`
	LightpandaObey      *bool     `json:"lightpanda_obey_robots"`
	TunnelMode          *string   `json:"tunnel_mode"`
	TunnelName          *string   `json:"tunnel_name"`
	TunnelDomain        *string   `json:"tunnel_domain"`
	TunnelProtocol      *string   `json:"tunnel_protocol"`
}

type slotUpdate struct {
	Enabled     *bool `json:"enabled"`
	AllowImages *bool `json:"allow_images"`
	RTK         *bool `json:"rtk"`
	Caveman     *bool `json:"caveman"`
}

type externalMCPInput struct {
	Name             *string            `json:"name"`
	Prefix           *string            `json:"prefix"`
	Transport        *string            `json:"transport"`
	Enabled          *bool              `json:"enabled"`
	Command          *string            `json:"command"`
	Args             *[]string          `json:"args"`
	WorkingDirectory *string            `json:"working_directory"`
	URL              *string            `json:"url"`
	Environment      *map[string]string `json:"environment"`
	Headers          *map[string]string `json:"headers"`
	TimeoutSeconds   *int               `json:"timeout_seconds"`
}

func registerDashboardRoutes(mux *http.ServeMux, runtime *applicationRuntime) {
	mux.HandleFunc("/", runtime.handleDashboard)
	mux.HandleFunc("/assets/", runtime.handleDashboardAsset)
	mux.HandleFunc("/api/state", runtime.requireAdmin(runtime.handleDashboardState))
	mux.HandleFunc("/api/token/reveal", runtime.requireAdminMutation(runtime.handleTokenReveal))
	mux.HandleFunc("/api/settings", runtime.requireAdminMutation(runtime.handleSettings))
	mux.HandleFunc("/api/mcp", runtime.requireAdminMutation(runtime.handleMCPCollection))
	mux.HandleFunc("/api/mcp/", runtime.requireAdminMutation(runtime.handleMCPItem))
	mux.HandleFunc("/api/agents", runtime.requireAdminMutation(runtime.handleAgentCollection))
	mux.HandleFunc("/api/agents/", runtime.requireAdminMutation(runtime.handleAgentItem))
	mux.HandleFunc("/api/runs/", runtime.requireAdminMutation(runtime.handleRunItem))
	mux.HandleFunc("/api/router/refresh", runtime.requireAdminMutation(runtime.handleRouterRefresh))
	mux.HandleFunc("/api/lightpanda/start", runtime.requireAdminMutation(runtime.handleLightpandaStart))
	mux.HandleFunc("/api/lightpanda/stop", runtime.requireAdminMutation(runtime.handleLightpandaStop))
	mux.HandleFunc("/api/tunnel/start", runtime.requireAdminMutation(runtime.handleTunnelStart))
	mux.HandleFunc("/api/tunnel/stop", runtime.requireAdminMutation(runtime.handleTunnelStop))
	mux.HandleFunc("/api/tunnel/dns", runtime.requireAdminMutation(runtime.handleTunnelDNS))
}

func (a *applicationRuntime) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if key := strings.TrimSpace(r.URL.Query().Get("admin")); key != "" {
		if !hmac.Equal([]byte(key), []byte(a.adminKey)) {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     dashboardCookieName,
			Value:    a.adminKey,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   12 * 60 * 60,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !a.authorizedAdmin(r) {
		http.NotFound(w, r)
		return
	}
	serveEmbeddedFile(w, "web/index.html", "text/html; charset=utf-8")
}

func (a *applicationRuntime) handleDashboardAsset(w http.ResponseWriter, r *http.Request) {
	if !a.authorizedAdmin(r) {
		http.NotFound(w, r)
		return
	}
	name := path.Base(r.URL.Path)
	switch name {
	case "app.css":
		serveEmbeddedFile(w, "web/app.css", "text/css; charset=utf-8")
	case "app.js":
		serveEmbeddedFile(w, "web/app.js", "application/javascript; charset=utf-8")
	case "icon.svg":
		serveEmbeddedFile(w, "web/icon.svg", "image/svg+xml")
	default:
		http.NotFound(w, r)
	}
}

func serveEmbeddedFile(w http.ResponseWriter, name, contentType string) {
	data, err := dashboardAssets.ReadFile(name)
	if err != nil {
		http.Error(w, "embedded asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	_, _ = w.Write(data)
}

func (a *applicationRuntime) authorizedAdmin(r *http.Request) bool {
	cookie, err := r.Cookie(dashboardCookieName)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(cookie.Value), []byte(a.adminKey))
}

func (a *applicationRuntime) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authorizedAdmin(r) {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

func (a *applicationRuntime) requireAdminMutation(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !hmac.Equal([]byte(r.Header.Get("X-Tautline-CSRF")), []byte(a.csrfToken)) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (a *applicationRuntime) handleDashboardState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.dashboardSnapshot())
}

func (a *applicationRuntime) dashboardSnapshot() dashboardState {
	cfg := a.config.snapshot()
	tunnel := a.tunnel.status()
	publicBaseURL := strings.TrimRight(cfg.PublicBaseURL, "/")
	if tunnel.PublicURL != "" {
		publicBaseURL = strings.TrimRight(tunnel.PublicURL, "/")
	}
	mcpPublicURL := ""
	if publicBaseURL != "" {
		mcpPublicURL = publicBaseURL + "/mcp"
	}
	return dashboardState{
		Service:      appName,
		Version:      appVersion,
		StartedAt:    a.startedAt,
		Uptime:       int64(time.Since(a.startedAt).Seconds()),
		CSRF:         a.csrfToken,
		LocalURL:     "http://127.0.0.1:" + cfg.Port,
		MCPLocalURL:  "http://127.0.0.1:" + cfg.Port + "/mcp",
		MCPPublicURL: mcpPublicURL,
		OwnerToken:   maskedSecret(ownerToken),
		AllowedRoots: append([]string(nil), allowedRoots...),
		Config: dashboardConfigView{
			Port:          cfg.Port,
			RuntimeDir:    cfg.RuntimeDir,
			WorktreeRoot:  effectiveWorktreeRoot(cfg),
			PublicBaseURL: cfg.PublicBaseURL,
			WidgetDomain:  cfg.WidgetDomain,
			AgentEnabled:  cfg.AgentEnabled,
			AgentBackend:  cfg.AgentBackend,
			Router: RouterConfigView{
				BaseURL:       cfg.Router.BaseURL,
				APIKey:        maskedSecret(cfg.Router.APIKey),
				DefaultModel:  cfg.Router.DefaultModel,
				AllowedModels: append([]string(nil), cfg.Router.AllowedModels...),
			},
			Lightpanda: cfg.Lightpanda,
			Tunnel:     cfg.Tunnel,
		},
		Router:      a.agents.routerStatusSnapshot(),
		RelayBridge: a.relayBridge.status(),
		Lightpanda:  a.lightpanda.status(),
		Tunnel:      tunnel,
		MCPServers:  a.mcpClients.statuses(),
		Slots:       sortedAgentSlots(a.agents.slotsSnapshot()),
		Runs:        a.agents.runsSnapshot(),
	}
}

func (a *applicationRuntime) handleTokenReveal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"token": ownerToken})
}

func (a *applicationRuntime) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input settingsUpdate
	if err := decodeJSONBody(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err := a.config.update(func(cfg *TautlineConfig) error {
		if input.RouterBaseURL != nil {
			cfg.Router.BaseURL = strings.TrimSpace(*input.RouterBaseURL)
		}
		if input.RouterAPIKey != nil {
			cfg.Router.APIKey = strings.TrimSpace(*input.RouterAPIKey)
		}
		if input.RouterDefaultModel != nil {
			cfg.Router.DefaultModel = strings.TrimSpace(*input.RouterDefaultModel)
		}
		if input.RouterAllowedModels != nil {
			cfg.Router.AllowedModels = append([]string(nil), (*input.RouterAllowedModels)...)
		}
		if input.AgentEnabled != nil {
			cfg.AgentEnabled = *input.AgentEnabled
		}
		if input.AgentBackend != nil {
			cfg.AgentBackend = strings.TrimSpace(*input.AgentBackend)
		}
		if input.LightpandaPath != nil {
			cfg.Lightpanda.Executable = strings.TrimSpace(*input.LightpandaPath)
		}
		if input.LightpandaPort != nil {
			cfg.Lightpanda.Port = *input.LightpandaPort
		}
		if input.LightpandaObey != nil {
			cfg.Lightpanda.ObeyRobots = *input.LightpandaObey
		}
		if input.TunnelMode != nil {
			cfg.Tunnel.Mode = strings.TrimSpace(*input.TunnelMode)
		}
		if input.TunnelName != nil {
			cfg.Tunnel.Name = strings.TrimSpace(*input.TunnelName)
		}
		if input.TunnelDomain != nil {
			cfg.Tunnel.CustomDomain = normalizeDomain(*input.TunnelDomain)
			if cfg.Tunnel.CustomDomain != "" {
				cfg.PublicBaseURL = "https://" + cfg.Tunnel.CustomDomain
			}
		}
		if input.TunnelProtocol != nil {
			cfg.Tunnel.Protocol = strings.TrimSpace(*input.TunnelProtocol)
		}
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if a.config.snapshot().AgentBackend == agentBackend9Router {
		go a.probeRouter(context.Background())
	}
	go a.lightpanda.probeRunner()
	writeJSON(w, http.StatusOK, a.dashboardSnapshot())
}

func normalizeDomain(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return strings.TrimSuffix(value, "/")
}

func (a *applicationRuntime) handleMCPCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input externalMCPInput
	if err := decodeJSONBody(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	config := ExternalMCPConfig{Transport: "stdio", TimeoutSeconds: defaultExternalMCPTimeout}
	applyExternalMCPInput(&config, input)
	status, err := a.mcpClients.addConfig(config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

func (a *applicationRuntime) handleMCPItem(w http.ResponseWriter, r *http.Request) {
	remainder := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mcp/"), "/")
	parts := strings.Split(remainder, "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var (
			status ExternalMCPStatus
			err    error
		)
		switch parts[1] {
		case "connect":
			status, err = a.mcpClients.setEnabled(r.Context(), id, true)
		case "disconnect":
			status, err = a.mcpClients.setEnabled(r.Context(), id, false)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if err := a.mcpClients.removeConfig(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"removed": id})
	case http.MethodPatch, http.MethodPost:
		config, exists := a.mcpClients.config(id)
		if !exists {
			http.NotFound(w, r)
			return
		}
		var input externalMCPInput
		if err := decodeJSONBody(r, &input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		applyExternalMCPInput(&config, input)
		status, err := a.mcpClients.updateConfig(config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func applyExternalMCPInput(config *ExternalMCPConfig, input externalMCPInput) {
	if input.Name != nil {
		config.Name = strings.TrimSpace(*input.Name)
	}
	if input.Prefix != nil {
		config.Prefix = strings.TrimSpace(*input.Prefix)
	}
	if input.Transport != nil {
		config.Transport = strings.TrimSpace(*input.Transport)
	}
	if input.Enabled != nil {
		config.Enabled = *input.Enabled
	}
	if input.Command != nil {
		config.Command = strings.TrimSpace(*input.Command)
	}
	if input.Args != nil {
		config.Args = append([]string(nil), (*input.Args)...)
	}
	if input.WorkingDirectory != nil {
		config.WorkingDirectory = strings.TrimSpace(*input.WorkingDirectory)
	}
	if input.URL != nil {
		config.URL = strings.TrimSpace(*input.URL)
	}
	if input.Environment != nil {
		config.Environment = cloneStringMap(*input.Environment)
	}
	if input.Headers != nil {
		config.Headers = cloneStringMap(*input.Headers)
	}
	if input.TimeoutSeconds != nil {
		config.TimeoutSeconds = *input.TimeoutSeconds
	}
}

func (a *applicationRuntime) handleAgentCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slot, err := a.agents.addSlot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, slot)
}

func (a *applicationRuntime) handleAgentItem(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := a.agents.removeSlot(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"removed": id})
	case http.MethodPatch, http.MethodPost:
		var input slotUpdate
		if err := decodeJSONBody(r, &input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slot, err := a.agents.updateSlot(id, input.Enabled, input.AllowImages, input.RTK, input.Caveman)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, slot)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *applicationRuntime) handleRunItem(w http.ResponseWriter, r *http.Request) {
	remainder := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/runs/"), "/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[1] != "cancel" {
		http.NotFound(w, r)
		return
	}
	if err := a.agents.cancelRun(parts[0]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.relayBridge.markRun(parts[0], "cancelled")
	run, _ := a.agents.getRun(parts[0])
	writeJSON(w, http.StatusOK, run)
}

func (a *applicationRuntime) handleRouterRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, a.agents.refreshRouterStatus(ctx))
}

func (a *applicationRuntime) handleLightpandaStart(w http.ResponseWriter, _ *http.Request) {
	if err := a.lightpanda.start(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, a.lightpanda.status())
}

func (a *applicationRuntime) handleLightpandaStop(w http.ResponseWriter, _ *http.Request) {
	if err := a.lightpanda.stop(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, a.lightpanda.status())
}

func (a *applicationRuntime) handleTunnelStart(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mode string `json:"mode"`
	}
	_ = decodeJSONBodyOptional(r, &input)
	if err := a.tunnel.start(input.Mode); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, a.tunnel.status())
}

func (a *applicationRuntime) handleTunnelStop(w http.ResponseWriter, _ *http.Request) {
	if err := a.tunnel.stop(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, a.tunnel.status())
}

func (a *applicationRuntime) handleTunnelDNS(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Domain string `json:"domain"`
	}
	if err := decodeJSONBody(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status, err := a.tunnel.routeDNS(input.Domain)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func decodeJSONBody(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func decodeJSONBodyOptional(r *http.Request, target any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return decodeJSONBody(r, target)
}

func contextWithTimeout(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, duration)
}
