package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keyValue := strings.SplitN(line, "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		key := strings.TrimSpace(keyValue[0])
		value := strings.Trim(strings.TrimSpace(keyValue[1]), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func envBool(name string, fallback bool) bool {
	return envBoolWithFallback(name, fallback)
}

func commandLineFlagPresent(name string) bool {
	prefix := "-" + name
	for _, argument := range os.Args[1:] {
		if argument == prefix || argument == "-"+prefix || strings.HasPrefix(argument, prefix+"=") || strings.HasPrefix(argument, "-"+prefix+"=") {
			return true
		}
	}
	return false
}

func main() {
	loadDotEnv()
	var store *configStore
	var err error
	if commandLineFlagPresent("doctor") {
		store, err = loadTautlineConfigReadOnly()
	} else {
		store, err = loadTautlineConfig()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}
	defaults := store.snapshot()
	start := flag.Bool("start", false, "start Tautline")
	stop := flag.Bool("stop", false, "stop the Tautline process for this port")
	doctor := flag.Bool("doctor", false, "run read-only Tautline diagnostics")
	openOnly := flag.Bool("open-dashboard", false, "open the dashboard for the running Tautline instance")
	authMCP := flag.String("auth-mcp", "", "authorize one OAuth-enabled external MCP connector")
	authGoogleDocs := flag.Bool("auth-google-docs", false, "authorize native Google Docs REST access")
	testGoogleDocsID := flag.String("test-google-docs", "", "read one Google Doc through the native REST integration")
	port := flag.String("port", defaults.Port, "local dashboard and MCP port")
	tunnelMode := flag.String("tunnel", "", "start Cloudflare tunnel mode: quick, named, or empty")
	openDashboard := flag.Bool("dashboard", defaults.OpenDashboard, "open the local dashboard")
	flag.Parse()

	switch {
	case *doctor:
		view := cliDoctorView(store, *port)
		printDoctor(view)
		if doctorHasErrors(view) {
			os.Exit(1)
		}
	case strings.TrimSpace(*authMCP) != "":
		if err := authorizeExternalMCP(store, *authMCP); err != nil {
			fmt.Fprintln(os.Stderr, "MCP OAuth authorization failed:", err)
			os.Exit(1)
		}
	case *authGoogleDocs:
		if err := authorizeGoogleDocs(store); err != nil {
			fmt.Fprintln(os.Stderr, "Google Docs OAuth authorization failed:", err)
			os.Exit(1)
		}
	case strings.TrimSpace(*testGoogleDocsID) != "":
		if err := testGoogleDocs(store, *testGoogleDocsID); err != nil {
			fmt.Fprintln(os.Stderr, "Google Docs native REST test failed:", err)
			os.Exit(1)
		}
	case *openOnly:
		doOpenDashboard(store, *port)
	case *stop:
		doStop(store, *port)
	case *start:
		doStart(store, *port, *tunnelMode, *openDashboard)
	default:
		fmt.Printf("usage: tautline -start|-stop|-doctor|-open-dashboard|-auth-mcp <id>|-auth-google-docs|-test-google-docs <document-id> [-port %s] [-dashboard=true] [-tunnel=quick|named]\n", defaults.Port)
	}
}

func doOpenDashboard(store *configStore, port string) {
	cfg := store.snapshot()
	port = strings.TrimSpace(port)
	if port == "" {
		port = cfg.Port
	}
	key, err := loadOrCreateDashboardKey(cfg.RuntimeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dashboard key error:", err)
		return
	}
	baseURL := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(baseURL + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Tautline is not reachable on port %s: %v\n", port, err)
		return
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Tautline health check on port %s returned %s\n", port, response.Status)
		return
	}
	if err := openLocalURL(baseURL + "/?admin=" + key); err != nil {
		fmt.Fprintln(os.Stderr, "could not open the Tautline dashboard:", err)
		return
	}
	fmt.Printf("Opened the Tautline dashboard on port %s.\n", port)
}

func doStart(store *configStore, port, requestedTunnelMode string, openDashboard bool) {
	if err := store.update(func(cfg *TautlineConfig) error {
		cfg.Port = strings.TrimSpace(port)
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		return
	}
	cfg := store.snapshot()
	setDefaultEnvironment("DEVSPACE_RUNTIME_DIR", cfg.RuntimeDir)
	setDefaultEnvironment("DEVSPACE_ARTIFACT_DIR", filepath.Join(cfg.RuntimeDir, "artifacts"))
	if widgets := strings.TrimSpace(os.Getenv("TAUTLINE_WIDGETS")); widgets != "" {
		setDefaultEnvironment("DEVSPACE_WIDGETS", widgets)
	}
	loadConfig()
	loadWorkflowConfig()
	if err := configureWorkspacePersistence(cfg.RuntimeDir, effectiveWorktreeRoot(cfg)); err != nil {
		fmt.Fprintln(os.Stderr, "workspace registry initialization failed:", err)
		return
	}
	oauth := newOAuth(cfg.RuntimeDir)

	app, err := newApplicationRuntime(store)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtime initialization failed:", err)
		return
	}
	setApplicationRuntime(app)

	primaryInstructions, instructionStatus, instructionErr := hostInstructions()
	if instructionErr != nil {
		fmt.Fprintln(os.Stderr, "Codex host instructions:", instructionErr, "Using Tautline instructions only.")
	}
	mcpServer := server.NewMCPServer(
		appName,
		appVersion,
		server.WithToolCapabilities(true),
		server.WithInstructions(primaryInstructions),
		server.WithToolHandlerMiddleware(activityMiddleware(app.activity)),
	)
	registerWidgetResource(mcpServer)
	registerTools(mcpServer)
	registerGoogleDocsTools(mcpServer, store)
	app.mcpClients.attachServer(mcpServer)
	if cfg.Lightpanda.NativeMCP {
		startupContext, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Lightpanda.NativeTimeoutSeconds)*time.Second)
		nativeTools, nativeErr := app.lightpanda.prepareNativeMCP(startupContext)
		cancel()
		if nativeErr != nil {
			fmt.Fprintln(os.Stderr, "Lightpanda native MCP initialization failed:", nativeErr)
			app.shutdown()
			return
		}
		if err := registerLightpandaProxyTools(mcpServer, nativeTools, app.lightpanda.callNativeRequest); err != nil {
			fmt.Fprintln(os.Stderr, "Lightpanda native MCP tool registration failed:", err)
			app.shutdown()
			return
		}
	}
	for _, connectorErr := range app.mcpClients.startConfigured(context.Background()) {
		fmt.Fprintln(os.Stderr, "External MCP connector:", connectorErr)
	}
	mcpHandler := server.NewStreamableHTTPServer(
		mcpServer,
		server.WithStateLess(true),
		server.WithDisableStreaming(true),
	)

	mux := http.NewServeMux()
	mux.Handle(canonicalMCPPath, oauth.requireBearer(mcpHandler))
	mux.Handle(versionedMCPPath, oauth.requireBearer(mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":            "ok",
			"service":           appName,
			"version":           appVersion,
			"widget":            activityWidgetURI,
			"tools":             len(mcpServer.ListTools()),
			"subagents":         len(app.agents.slotsSnapshot()),
			"agent_backend":     cfg.AgentBackend,
			"mcp_clients":       app.mcpClients.summary(),
			"google_docs":       googleDocsHealth(store),
			"host_instructions": instructionStatus,
			"router":            app.agents.routerStatusSnapshot(),
			"relay_bridge":      app.relayBridge.status(),
			"lightpanda":        app.lightpanda.status(),
			"tunnel":            app.tunnel.status(),
		})
	})
	registerOAuthRoutes(mux, oauth)
	registerRelayBridgeRoutes(mux, app.relayBridge)
	registerDashboardRoutes(mux, app)

	address := "127.0.0.1:" + cfg.Port
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Printf("listen failed on %s: %v\n", address, err)
		return
	}
	defer listener.Close()

	pidPath := pidFileForPort(store, cfg.Port)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		fmt.Println("warning: could not create runtime directory:", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o600); err != nil {
		fmt.Println("warning: could not write pid file:", err)
	}
	defer os.Remove(pidPath)

	httpServer := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       0,
		MaxHeaderBytes:    1 << 20,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.Serve(listener) }()
	app.startProbes()

	fmt.Printf("%s %s listening on http://%s/mcp\n", appName, appVersion, address)
	fmt.Printf("Dashboard: http://%s/\n", address)
	fmt.Printf("Health: http://%s/healthz\n", address)
	fmt.Printf("Allowed roots: %s\n", strings.Join(allowedRoots, ", "))
	fmt.Printf("Sub-agent capacity: %d generic slots through %s\n", len(app.agents.slotsSnapshot()), cfg.AgentBackend)
	fmt.Printf("Widget resource: %s\n", activityWidgetURI)
	if openDashboard {
		dashboardURL := fmt.Sprintf("http://%s/?admin=%s", address, app.adminKey)
		if err := openLocalURL(dashboardURL); err != nil {
			fmt.Printf("Open this one-time local dashboard URL: %s\n", dashboardURL)
		}
	}

	if cfg.Lightpanda.AutoStart {
		go func() {
			if err := app.lightpanda.start(); err != nil {
				fmt.Println("Lightpanda autostart:", err)
			}
		}()
	}
	mode := effectiveTunnelMode(requestedTunnelMode, cfg.Tunnel)
	if mode == "quick" || mode == "named" {
		go func() {
			if err := app.tunnel.start(mode); err != nil {
				fmt.Println("Cloudflare tunnel autostart:", err)
			}
		}()
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case signalValue := <-signals:
		fmt.Printf("\nreceived %s, shutting down...\n", signalValue)
	case err := <-serveErrors:
		if err != nil && err != http.ErrServerClosed {
			fmt.Println("server stopped:", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	app.shutdown()
	_ = httpServer.Shutdown(shutdownContext)
}

func effectiveTunnelMode(requested string, cfg TunnelConfig) string {
	mode := strings.ToLower(strings.TrimSpace(requested))
	if mode != "" {
		return mode
	}
	if !cfg.AutoStart {
		return ""
	}
	mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "off" || mode == "" {
		if strings.TrimSpace(cfg.Name) != "" {
			return "named"
		}
		return "quick"
	}
	return mode
}

func doStop(store *configStore, port string) {
	pidPath := pidFileForPort(store, port)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Println("no Tautline instance recorded for port", port)
		return
	}
	pid := strings.TrimSpace(string(data))
	fmt.Println("stopping Tautline pid", pid)
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", pid, "/F", "/T").Run()
	} else {
		_ = exec.Command("kill", "-TERM", pid).Run()
	}
	_ = os.Remove(pidPath)
}

func pidFileForPort(store *configStore, port string) string {
	runtimeDir := store.snapshot().RuntimeDir
	return filepath.Join(runtimeDir, ".run-"+strings.TrimSpace(port)+".pid")
}

func setDefaultEnvironment(key, value string) {
	if strings.TrimSpace(os.Getenv(key)) == "" && strings.TrimSpace(value) != "" {
		_ = os.Setenv(key, value)
	}
}

func openLocalURL(rawURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		command = exec.Command("open", rawURL)
	default:
		command = exec.Command("xdg-open", rawURL)
	}
	return command.Start()
}

func mustWorkingDirectory() string {
	directory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return directory
}
