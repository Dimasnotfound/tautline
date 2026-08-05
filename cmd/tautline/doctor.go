package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Action  string `json:"action,omitempty"`
}

type doctorMCPCounts struct {
	Configured int `json:"configured"`
	Connected  int `json:"connected"`
	Tools      int `json:"tools"`
}

type doctorView struct {
	Kind                 string                `json:"kind"`
	Title                string                `json:"title"`
	Summary              string                `json:"summary"`
	Version              string                `json:"version"`
	Running              bool                  `json:"running"`
	RunningVersion       string                `json:"running_version,omitempty"`
	VersionMatch         bool                  `json:"version_match"`
	Port                 string                `json:"port"`
	RuntimeDir           string                `json:"runtime_dir"`
	WorktreeRoot         string                `json:"worktree_root"`
	ConfigPath           string                `json:"config_path"`
	ConfigExists         bool                  `json:"config_exists"`
	AllowedRoots         []string              `json:"allowed_roots"`
	AuthRequired         bool                  `json:"auth_required"`
	OwnerTokenConfigured bool                  `json:"owner_token_configured"`
	PublishedTools       int                   `json:"published_tools"`
	MCP                  doctorMCPCounts       `json:"mcp"`
	MCPServers           []ExternalMCPStatus   `json:"mcp_servers,omitempty"`
	GoogleDocs           googleDocsHealthView  `json:"google_docs"`
	Router               RouterStatus          `json:"router"`
	Lightpanda           LightpandaStatus      `json:"lightpanda"`
	Tunnel               TunnelStatus          `json:"tunnel"`
	HostInstructions     hostInstructionStatus `json:"host_instructions"`
	Checks               []doctorCheck         `json:"checks"`
	Actions              []string              `json:"actions,omitempty"`
}

type doctorHealthResponse struct {
	Status           string                `json:"status"`
	Service          string                `json:"service"`
	Version          string                `json:"version"`
	Tools            int                   `json:"tools"`
	MCPClients       doctorMCPCounts       `json:"mcp_clients"`
	GoogleDocs       googleDocsHealthView  `json:"google_docs"`
	Router           RouterStatus          `json:"router"`
	Lightpanda       LightpandaStatus      `json:"lightpanda"`
	Tunnel           TunnelStatus          `json:"tunnel"`
	HostInstructions hostInstructionStatus `json:"host_instructions"`
}

func registerDoctorTool(s *server.MCPServer) {
	tool := mcp.NewTool("tautline_doctor",
		mcp.WithTitleAnnotation("Diagnose Tautline"),
		mcp.WithDescription("Run a read-only Tautline diagnostic summary for version, configuration, allowed roots, OAuth readiness, Google Docs, external MCP connections, published tools, 9Router, Lightpanda, tunnel, and concrete corrective actions. Secret values are never returned."),
		mcp.WithOutputSchema[doctorView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(tool, handleTautlineDoctor)
}

func handleTautlineDoctor(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := runtimeDoctorView(runtime)
	return newToolResult("tautline_doctor", view, view, view.Summary), nil
}

func runtimeDoctorView(runtime *applicationRuntime) doctorView {
	view := newDoctorView(runtime.config)
	view.Running = true
	view.RunningVersion = appVersion
	view.VersionMatch = true
	view.MCPServers = runtime.mcpClients.statuses()
	view.MCP = countMCPStatuses(view.MCPServers)
	view.Router = runtime.agents.routerStatusSnapshot()
	view.Lightpanda = runtime.lightpanda.status()
	view.Tunnel = runtime.tunnel.status()
	_, view.HostInstructions, _ = hostInstructions()
	if mcpServer := runtime.mcpClients.attachedServer(); mcpServer != nil {
		view.PublishedTools = len(mcpServer.ListTools())
	}
	appendRuntimeDoctorChecks(&view, runtime.config.snapshot())
	finishDoctorView(&view)
	return view
}

func cliDoctorView(store *configStore, port string) doctorView {
	view := newDoctorView(store)
	port = strings.TrimSpace(port)
	if port == "" {
		port = store.snapshot().Port
	}
	view.Port = port
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		addDoctorCheck(&view, "runtime", "error", "Tautline is not reachable on port "+port, "Start Tautline with -start or START_TAUTLINE.cmd.")
		finishDoctorView(&view)
		return view
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		addDoctorCheck(&view, "runtime", "error", "healthz returned "+response.Status, "Inspect the Tautline console and runtime logs.")
		finishDoctorView(&view)
		return view
	}
	var health doctorHealthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		addDoctorCheck(&view, "runtime", "error", "healthz returned invalid JSON", "Restart Tautline and inspect the console output.")
		finishDoctorView(&view)
		return view
	}
	view.Running = health.Status == "ok" && health.Service == appName
	view.RunningVersion = health.Version
	view.VersionMatch = health.Version == appVersion
	view.PublishedTools = health.Tools
	view.MCP = health.MCPClients
	view.GoogleDocs = health.GoogleDocs
	view.Router = health.Router
	view.Lightpanda = health.Lightpanda
	view.Tunnel = health.Tunnel
	view.HostInstructions = health.HostInstructions
	if view.Running {
		addDoctorCheck(&view, "runtime", "ok", "Tautline health endpoint is ready", "")
	} else {
		addDoctorCheck(&view, "runtime", "error", "health endpoint did not identify a healthy Tautline runtime", "Restart Tautline and inspect the console output.")
	}
	if view.VersionMatch {
		addDoctorCheck(&view, "version", "ok", "active runtime matches Tautline "+appVersion, "")
	} else {
		addDoctorCheck(&view, "version", "error", fmt.Sprintf("active runtime is %s; source expects %s", health.Version, appVersion), "Run SWITCH_TO_TAUTLINE.cmd to activate the latest built version.")
	}
	appendRuntimeDoctorChecks(&view, store.snapshot())
	finishDoctorView(&view)
	return view
}

func newDoctorView(store *configStore) doctorView {
	cfg := store.snapshot()
	view := doctorView{
		Kind:                 "doctor",
		Title:                "Tautline doctor",
		Version:              appVersion,
		Port:                 cfg.Port,
		RuntimeDir:           cfg.RuntimeDir,
		WorktreeRoot:         effectiveWorktreeRoot(cfg),
		ConfigPath:           store.path,
		AllowedRoots:         doctorAllowedRoots(),
		AuthRequired:         doctorAuthRequired(),
		OwnerTokenConfigured: doctorOwnerTokenConfigured(),
		GoogleDocs:           googleDocsHealth(store),
	}
	if info, err := os.Stat(store.path); err == nil && !info.IsDir() {
		view.ConfigExists = true
		addDoctorCheck(&view, "configuration", "ok", "configuration file is readable", "")
	} else {
		addDoctorCheck(&view, "configuration", "warn", "configuration file does not exist yet", "Run scripts/setup.ps1 or start Tautline once to create it.")
	}
	if info, err := os.Stat(cfg.RuntimeDir); err == nil && info.IsDir() {
		addDoctorCheck(&view, "runtime directory", "ok", "runtime directory is available", "")
	} else {
		addDoctorCheck(&view, "runtime directory", "warn", "runtime directory does not exist yet", "Start Tautline once to initialize runtime state.")
	}
	if info, err := os.Stat(view.WorktreeRoot); err == nil && info.IsDir() {
		addDoctorCheck(&view, "worktree root", "ok", "managed Git worktree root is available", "")
	} else {
		addDoctorCheck(&view, "worktree root", "warn", "managed Git worktree root does not exist yet", "It will be created when the first worktree workspace is opened.")
	}
	if len(view.AllowedRoots) > 0 {
		addDoctorCheck(&view, "allowed roots", "ok", fmt.Sprintf("%d allowed roots configured", len(view.AllowedRoots)), "")
	} else {
		addDoctorCheck(&view, "allowed roots", "error", "no valid allowed roots are configured", "Set TAUTLINE_ALLOWED_ROOTS to one or more intended project directories.")
	}
	if view.AuthRequired {
		addDoctorCheck(&view, "OAuth protection", "ok", "bearer authentication is required", "")
	} else {
		addDoctorCheck(&view, "OAuth protection", "error", "bearer authentication is disabled", "Set TAUTLINE_REQUIRE_AUTH=true before exposing the MCP endpoint.")
	}
	if view.OwnerTokenConfigured {
		addDoctorCheck(&view, "owner token", "ok", "a persistent owner token is configured", "")
	} else {
		addDoctorCheck(&view, "owner token", "warn", "no persistent owner token is configured", "Set TAUTLINE_OWNER_TOKEN to a unique random value.")
	}
	if view.GoogleDocs.Enabled && !view.GoogleDocs.TokenStored {
		addDoctorCheck(&view, "Google Docs", "warn", "native Google Docs is enabled without a stored token", "Run tautline -auth-google-docs.")
	} else if view.GoogleDocs.Enabled {
		addDoctorCheck(&view, "Google Docs", "ok", "native Google Docs token is stored", "")
	}
	return view
}

func appendRuntimeDoctorChecks(view *doctorView, cfg TautlineConfig) {
	if view.PublishedTools > 0 {
		addDoctorCheck(view, "MCP tools", "ok", fmt.Sprintf("%d tools are published", view.PublishedTools), "")
	} else {
		addDoctorCheck(view, "MCP tools", "warn", "published tool count is unavailable", "Reconnect the Tautline MCP application if tools are missing.")
	}
	if view.MCP.Configured > 0 && view.MCP.Connected < view.MCP.Configured {
		addDoctorCheck(view, "external MCP", "warn", fmt.Sprintf("%d of %d integrations are connected", view.MCP.Connected, view.MCP.Configured), "Open the dashboard and reconnect integrations that show an error.")
	} else if view.MCP.Configured > 0 {
		addDoctorCheck(view, "external MCP", "ok", fmt.Sprintf("all %d integrations are connected", view.MCP.Configured), "")
	}
	if cfg.AgentEnabled && !view.Router.Reachable {
		addDoctorCheck(view, "9Router", "warn", "9Router is not reachable", "Start 9Router or disable sub-agent delegation when it is not needed.")
	} else if cfg.AgentEnabled {
		addDoctorCheck(view, "9Router", "ok", fmt.Sprintf("9Router is reachable with %d models", len(view.Router.Models)), "")
	}
	if cfg.Lightpanda.NativeMCP && !view.Lightpanda.NativeMCPReady {
		addDoctorCheck(view, "Lightpanda", "warn", "native Lightpanda MCP is not ready", "Check the configured Lightpanda executable, Docker, or WSL runtime.")
	} else if cfg.Lightpanda.NativeMCP {
		addDoctorCheck(view, "Lightpanda", "ok", fmt.Sprintf("native MCP exposes %d tools", view.Lightpanda.NativeMCPTools), "")
	}
	if effectiveTunnelMode("", cfg.Tunnel) != "" && !view.Tunnel.Running {
		addDoctorCheck(view, "tunnel", "warn", "configured tunnel is not running", "Start the tunnel from the dashboard or with -tunnel.")
	} else if view.Tunnel.Running {
		addDoctorCheck(view, "tunnel", "ok", "Cloudflare tunnel is running", "")
	}
}

func countMCPStatuses(statuses []ExternalMCPStatus) doctorMCPCounts {
	counts := doctorMCPCounts{Configured: len(statuses)}
	for _, status := range statuses {
		if status.Connected {
			counts.Connected++
		}
		counts.Tools += status.ToolCount
	}
	return counts
}

func doctorAllowedRoots() []string {
	if len(allowedRoots) > 0 {
		return append([]string(nil), allowedRoots...)
	}
	raw := firstEnvironment("TAUTLINE_ALLOWED_ROOTS", "DEVSPACE_ALLOWED_ROOTS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	roots := make([]string, 0, strings.Count(raw, ",")+1)
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if canonical, err := canonicalPath(value); err == nil {
			roots = append(roots, canonical)
		}
	}
	return roots
}

func doctorAuthRequired() bool {
	value := strings.TrimSpace(firstEnvironment("TAUTLINE_REQUIRE_AUTH", "DEVSPACE_REQUIRE_AUTH"))
	return !strings.EqualFold(value, "false") && value != "0"
}

func doctorOwnerTokenConfigured() bool {
	if ownerToken != "" {
		return !ownerTokenGenerated
	}
	return strings.TrimSpace(firstEnvironment("TAUTLINE_OWNER_TOKEN", "DEVSPACE_OWNER_TOKEN")) != ""
}

func addDoctorCheck(view *doctorView, name, status, summary, action string) {
	view.Checks = append(view.Checks, doctorCheck{Name: name, Status: status, Summary: summary, Action: action})
	if action != "" {
		for _, existing := range view.Actions {
			if existing == action {
				return
			}
		}
		view.Actions = append(view.Actions, action)
	}
}

func finishDoctorView(view *doctorView) {
	ok, warnings, failures := 0, 0, 0
	for _, check := range view.Checks {
		switch check.Status {
		case "ok":
			ok++
		case "warn":
			warnings++
		case "error":
			failures++
		}
	}
	view.Summary = fmt.Sprintf("%d ok · %d warnings · %d errors", ok, warnings, failures)
}

func printDoctor(view doctorView) {
	fmt.Printf("Tautline doctor %s\n", view.Version)
	fmt.Printf("Runtime: %s", map[bool]string{true: "running", false: "offline"}[view.Running])
	if view.RunningVersion != "" {
		fmt.Printf(" · %s", view.RunningVersion)
	}
	fmt.Println()
	for _, check := range view.Checks {
		fmt.Printf("[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Summary)
	}
	if len(view.Actions) > 0 {
		fmt.Println("Actions:")
		for _, action := range view.Actions {
			fmt.Println("-", action)
		}
	}
	fmt.Println("Summary:", view.Summary)
}

func doctorHasErrors(view doctorView) bool {
	for _, check := range view.Checks {
		if check.Status == "error" {
			return true
		}
	}
	return false
}
