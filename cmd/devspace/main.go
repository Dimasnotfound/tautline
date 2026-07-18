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

const defaultPort = "7676"

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
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func main() {
	loadDotEnv()
	start := flag.Bool("start", false, "start the MCP server")
	stop := flag.Bool("stop", false, "stop a running server")
	port := flag.String("port", defaultPort, "local port")
	tunnel := flag.Bool("tunnel", envBool("DEVSPACE_START_TUNNEL", false), "start a configured Cloudflare named tunnel")
	flag.Parse()

	switch {
	case *stop:
		doStop(*port)
	case *start:
		doStart(*port, *tunnel)
	default:
		fmt.Println("usage: devspace -start [-port 7676] [-tunnel=true]")
	}
}

func doStart(port string, enableTunnel bool) {
	loadConfig()
	oauth := newOAuth()

	mcpServer := server.NewMCPServer(
		appName,
		appVersion,
		server.WithToolCapabilities(true),
	)
	registerWidgetResource(mcpServer)
	registerTools(mcpServer)

	mcpHandler := server.NewStreamableHTTPServer(
		mcpServer,
		server.WithStateLess(true),
		server.WithDisableStreaming(true),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", oauth.requireBearer(mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ok","service":%q,"version":%q}`, appName, appVersion)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", oauth.protectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server", oauth.authorizationServerMetadata)
	mux.HandleFunc("/register", oauth.register)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			oauth.authorizationPost(w, r)
			return
		}
		oauth.authorization(w, r)
	})
	mux.HandleFunc("/token", oauth.token)

	address := "127.0.0.1:" + port
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Printf("listen failed on %s: %v\n", address, err)
		return
	}
	defer listener.Close()

	pidPath := pidFileForPort(port)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		fmt.Println("warning: could not create runtime directory:", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o600); err != nil {
		fmt.Println("warning: could not write pid file:", err)
	}
	defer os.Remove(pidPath)

	var tunnelCommand *exec.Cmd
	if enableTunnel {
		tunnelCommand = startTunnel(port)
	}

	httpServer := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.Serve(listener) }()

	fmt.Printf("%s %s listening on http://%s/mcp\n", appName, appVersion, address)
	fmt.Printf("Health check: http://%s/healthz\n", address)
	if publicURL := strings.TrimSpace(os.Getenv("DEVSPACE_PUBLIC_BASE_URL")); publicURL != "" {
		fmt.Printf("Public URL: %s/mcp\n", strings.TrimRight(publicURL, "/"))
	}
	if ownerTokenGenerated || envBool("DEVSPACE_SHOW_OWNER_TOKEN", false) {
		fmt.Printf("Owner token: %s\n", ownerToken)
	} else {
		fmt.Println("Owner token: configured and hidden (set DEVSPACE_SHOW_OWNER_TOKEN=true to display it)")
	}
	fmt.Printf("Allowed roots: %s\n", strings.Join(allowedRoots, ", "))

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	if tunnelCommand != nil && tunnelCommand.Process != nil {
		killProcessTree(tunnelCommand.Process.Pid)
	}
}

func doStop(port string) {
	pidPath := pidFileForPort(port)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Println("no running instance for port", port)
		return
	}
	pid := strings.TrimSpace(string(data))
	fmt.Println("stopping pid", pid)
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", pid, "/F", "/T").Run()
	} else {
		_ = exec.Command("kill", "-TERM", pid).Run()
	}
	_ = os.Remove(pidPath)
}

func pidFileForPort(port string) string {
	runtimeDir := strings.TrimSpace(os.Getenv("DEVSPACE_RUNTIME_DIR"))
	if runtimeDir == "" {
		runtimeDir = "runtime"
	}
	if port == defaultPort {
		return filepath.Join(runtimeDir, ".run.pid")
	}
	return filepath.Join(runtimeDir, ".run-"+port+".pid")
}

func startTunnel(port string) *exec.Cmd {
	tunnelName := strings.TrimSpace(os.Getenv("DEVSPACE_TUNNEL_NAME"))
	if tunnelName == "" {
		fmt.Println("tunnel not started: set DEVSPACE_TUNNEL_NAME or run with -tunnel=false")
		return nil
	}

	executable := strings.TrimSpace(os.Getenv("DEVSPACE_CLOUDFLARED_PATH"))
	if executable == "" {
		if found, err := exec.LookPath("cloudflared"); err == nil {
			executable = found
		} else {
			localName := "cloudflared"
			if runtime.GOOS == "windows" {
				localName += ".exe"
			}
			executable = filepath.Join("bin", localName)
		}
	}

	args := []string{"tunnel"}
	if protocol := strings.TrimSpace(os.Getenv("DEVSPACE_TUNNEL_PROTOCOL")); protocol != "" {
		args = append(args, "--protocol", protocol)
	}
	args = append(args, "--url", "http://127.0.0.1:"+port, "run", tunnelName)

	command := exec.Command(executable, args...)
	command.Dir = mustWorkingDirectory()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fmt.Println("tunnel start failed:", err)
		return nil
	}
	return command
}

func mustWorkingDirectory() string {
	directory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return directory
}
