package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type lightpandaMCPLaunch struct {
	Command string
	Args    []string
	Env     []string
	WorkDir string
}

type lightpandaToolCaller func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

func publicLightpandaToolName(nativeName string) string {
	var builder strings.Builder
	builder.WriteString("lightpanda_")
	input := []rune(strings.TrimSpace(nativeName))
	lastUnderscore := true
	for index, current := range input {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			if unicode.IsUpper(current) {
				previousIsLowerOrDigit := index > 0 && (unicode.IsLower(input[index-1]) || unicode.IsDigit(input[index-1]))
				nextIsLower := index+1 < len(input) && unicode.IsLower(input[index+1])
				if !lastUnderscore && (previousIsLowerOrDigit || nextIsLower) {
					builder.WriteByte('_')
				}
				current = unicode.ToLower(current)
			}
			builder.WriteRune(current)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.TrimRight(builder.String(), "_")
}

func buildLightpandaMCPLaunch(runner lightpandaRunner, cfg LightpandaConfig, runtimeDir string) (lightpandaMCPLaunch, error) {
	if !cfg.NativeMCP {
		return lightpandaMCPLaunch{}, errors.New("Lightpanda native MCP is disabled")
	}
	if strings.TrimSpace(runner.executable) == "" {
		return lightpandaMCPLaunch{}, errors.New("Lightpanda MCP runner has no executable")
	}

	runtimeDir = filepath.Clean(runtimeDir)
	workDir := filepath.Join(runtimeDir, "lightpanda")
	stateDir := filepath.Join(runtimeDir, "state", "lightpanda")
	cacheDir := filepath.Join(runtimeDir, "cache", "lightpanda")
	for _, directory := range []string{workDir, stateDir, cacheDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return lightpandaMCPLaunch{}, fmt.Errorf("create Lightpanda MCP directory %s: %w", directory, err)
		}
	}

	browserArgs := []string{"mcp", "--log-level", "error"}
	if cfg.ObeyRobots {
		browserArgs = append(browserArgs, "--obey-robots")
	}
	if cfg.BlockPrivateNetworks {
		browserArgs = append(browserArgs, "--block-private-networks")
	}
	if cfg.PersistSession && runner.mode == "binary" {
		cookiePath := filepath.Join(stateDir, "cookies.json")
		storagePath := filepath.Join(stateDir, "storage.sqlite")
		if info, err := os.Stat(cookiePath); err == nil && !info.IsDir() {
			browserArgs = append(browserArgs, "--cookie", cookiePath)
		}
		browserArgs = append(browserArgs,
			"--cookie-jar", cookiePath,
			"--storage-engine", "sqlite",
			"--storage-sqlite-path", storagePath,
			"--http-cache-dir", cacheDir,
		)
	}

	launch := lightpandaMCPLaunch{
		Env:     []string{"LIGHTPANDA_DISABLE_TELEMETRY=true"},
		WorkDir: workDir,
	}
	switch runner.mode {
	case "docker":
		launch.Command = runner.executable
		launch.Args = append([]string{
			"run", "--rm", "-i", "--name", runner.container + "-mcp",
			runner.dockerImage,
		}, browserArgs...)
	case "wsl":
		launch.Command = runner.executable
		launch.Args = []string{"sh", "-lc", "exec lightpanda " + shellJoin(browserArgs)}
	default:
		launch.Command = runner.executable
		launch.Args = browserArgs
	}
	return launch, nil
}

func lightpandaChildEnvironment(extra []string) []string {
	allowed := map[string]bool{
		"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true,
		"TEMP": true, "TMP": true, "TMPDIR": true, "HOME": true, "USER": true, "USERPROFILE": true,
		"LANG": true, "LC_ALL": true, "TERM": true, "SHELL": true,
		"TAUTLINE_LIGHTPANDA_WSL_DISTRO": true, "TAUTLINE_LIGHTPANDA_WSL_PATH": true,
		"BRAVE_API_KEY": true, "TAVILY_API_KEY": true,
	}
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		upper := strings.ToUpper(key)
		if allowed[upper] || strings.HasPrefix(upper, "LP_") {
			values[key] = parts[1]
		}
	}
	for _, entry := range extra {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			values[parts[0]] = parts[1]
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func registerLightpandaProxyTools(mcpServer *server.MCPServer, nativeTools []mcp.Tool, caller lightpandaToolCaller) error {
	if mcpServer == nil {
		return errors.New("MCP server is nil")
	}
	if caller == nil {
		return errors.New("Lightpanda tool caller is nil")
	}
	seen := make(map[string]string, len(nativeTools))
	for _, nativeTool := range nativeTools {
		nativeName := strings.TrimSpace(nativeTool.Name)
		if nativeName == "" {
			continue
		}
		publicName := publicLightpandaToolName(nativeName)
		if previous, exists := seen[publicName]; exists {
			return fmt.Errorf("Lightpanda tools %q and %q map to the same public name %q", previous, nativeName, publicName)
		}
		seen[publicName] = nativeName

		schema := nativeTool.RawInputSchema
		if len(schema) == 0 {
			encoded, err := json.Marshal(nativeTool.InputSchema)
			if err != nil {
				return fmt.Errorf("encode Lightpanda tool schema %s: %w", nativeName, err)
			}
			schema = encoded
		}
		description := strings.TrimSpace(nativeTool.Description)
		if description == "" {
			description = "Call the Lightpanda native MCP tool " + nativeName + "."
		} else {
			description = "Lightpanda native browser: " + description
		}
		tool := mcp.NewToolWithRawSchema(publicName, description, schema)
		tool.Annotations = nativeTool.Annotations
		capturedNativeName := nativeName
		mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			request.Params.Name = capturedNativeName
			return caller(ctx, request)
		})
	}
	return nil
}

func (m *lightpandaManager) prepareNativeMCP(ctx context.Context) ([]mcp.Tool, error) {
	cfg := m.store.snapshot()
	if !cfg.Lightpanda.NativeMCP {
		return nil, errors.New("Lightpanda native MCP is disabled")
	}
	m.nativeInitMu.Lock()
	defer m.nativeInitMu.Unlock()

	m.nativeMu.RLock()
	if m.nativeClient != nil && len(m.nativeTools) > 0 {
		tools := append([]mcp.Tool(nil), m.nativeTools...)
		m.nativeMu.RUnlock()
		return tools, nil
	}
	m.nativeMu.RUnlock()

	runner, err := m.resolveRunner()
	if err != nil {
		m.setNativeMCPError(err)
		return nil, err
	}
	launch, err := buildLightpandaMCPLaunch(runner, cfg.Lightpanda, cfg.RuntimeDir)
	if err != nil {
		m.setNativeMCPError(err)
		return nil, err
	}
	commandFactory := transport.WithCommandFunc(func(commandContext context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
		process := exec.CommandContext(commandContext, command, args...)
		process.Dir = launch.WorkDir
		process.Env = lightpandaChildEnvironment(env)
		return process, nil
	})
	client, err := mcpclient.NewStdioMCPClientWithOptions(launch.Command, launch.Env, launch.Args, commandFactory)
	if err != nil {
		m.setNativeMCPError(err)
		return nil, fmt.Errorf("start Lightpanda native MCP: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = client.Close()
		}
	}()

	timeout := time.Duration(cfg.Lightpanda.NativeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	initializeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	initialize := mcp.InitializeRequest{}
	initialize.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initialize.Params.ClientInfo = mcp.Implementation{Name: appName, Version: appVersion}
	serverInfo, err := client.Initialize(initializeContext, initialize)
	if err != nil {
		m.setNativeMCPError(err)
		return nil, fmt.Errorf("initialize Lightpanda native MCP: %w", err)
	}
	listed, err := client.ListTools(initializeContext, mcp.ListToolsRequest{})
	if err != nil {
		m.setNativeMCPError(err)
		return nil, fmt.Errorf("list Lightpanda native MCP tools: %w", err)
	}
	if len(listed.Tools) == 0 {
		err = errors.New("Lightpanda native MCP returned no tools")
		m.setNativeMCPError(err)
		return nil, err
	}

	client.OnConnectionLost(func(connectionErr error) {
		m.nativeMu.Lock()
		defer m.nativeMu.Unlock()
		if m.nativeClient == client {
			m.nativeClient = nil
			m.nativeTools = nil
			if connectionErr != nil {
				m.nativeLastError = connectionErr.Error()
			} else {
				m.nativeLastError = "Lightpanda native MCP connection closed"
			}
		}
	})
	m.nativeMu.Lock()
	m.nativeClient = client
	m.nativeTools = append([]mcp.Tool(nil), listed.Tools...)
	m.nativeServerName = serverInfo.ServerInfo.Name
	m.nativeServerVersion = serverInfo.ServerInfo.Version
	m.nativeLastError = ""
	m.nativeMu.Unlock()
	closeOnError = false
	return append([]mcp.Tool(nil), listed.Tools...), nil
}

func (m *lightpandaManager) callNativeTool(ctx context.Context, name string, arguments any) (*mcp.CallToolResult, error) {
	request := mcp.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = arguments
	return m.callNativeRequest(ctx, request)
}

func (m *lightpandaManager) callNativeRequest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := m.nativeClientForCall(ctx)
	if err != nil {
		return nil, err
	}
	result, callErr := client.CallTool(ctx, request)
	if callErr == nil {
		return result, nil
	}
	m.discardNativeClient(client, callErr)
	client, err = m.nativeClientForCall(ctx)
	if err != nil {
		return nil, fmt.Errorf("Lightpanda native MCP call %s failed and reconnect failed: %w", request.Params.Name, err)
	}
	result, callErr = client.CallTool(ctx, request)
	if callErr != nil {
		m.discardNativeClient(client, callErr)
		return nil, fmt.Errorf("Lightpanda native MCP call %s failed: %w", request.Params.Name, callErr)
	}
	return result, nil
}

func (m *lightpandaManager) nativeClientForCall(ctx context.Context) (*mcpclient.Client, error) {
	m.nativeMu.RLock()
	client := m.nativeClient
	m.nativeMu.RUnlock()
	if client != nil {
		return client, nil
	}
	if _, err := m.prepareNativeMCP(ctx); err != nil {
		return nil, err
	}
	m.nativeMu.RLock()
	client = m.nativeClient
	m.nativeMu.RUnlock()
	if client == nil {
		return nil, errors.New("Lightpanda native MCP client was not created")
	}
	return client, nil
}

func (m *lightpandaManager) discardNativeClient(client *mcpclient.Client, cause error) {
	m.nativeInitMu.Lock()
	defer m.nativeInitMu.Unlock()
	m.nativeMu.Lock()
	if m.nativeClient != client {
		m.nativeMu.Unlock()
		return
	}
	m.nativeClient = nil
	m.nativeTools = nil
	if cause != nil {
		m.nativeLastError = cause.Error()
	}
	m.nativeMu.Unlock()
	_ = client.Close()
}

func (m *lightpandaManager) setNativeMCPError(err error) {
	m.nativeMu.Lock()
	defer m.nativeMu.Unlock()
	if err == nil {
		m.nativeLastError = ""
		return
	}
	m.nativeLastError = err.Error()
}

func (m *lightpandaManager) nativeMCPStatus() (ready bool, toolCount int, serverName, serverVersion, lastError string) {
	m.nativeMu.RLock()
	defer m.nativeMu.RUnlock()
	return m.nativeClient != nil && len(m.nativeTools) > 0, len(m.nativeTools), m.nativeServerName, m.nativeServerVersion, m.nativeLastError
}

func (m *lightpandaManager) closeNativeMCP() error {
	m.nativeInitMu.Lock()
	defer m.nativeInitMu.Unlock()
	m.nativeMu.Lock()
	client := m.nativeClient
	m.nativeClient = nil
	m.nativeTools = nil
	m.nativeMu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}
