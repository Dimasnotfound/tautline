package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const defaultExternalMCPTimeout = 30

type ExternalMCPStatus struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Prefix           string   `json:"prefix"`
	Transport        string   `json:"transport"`
	ActiveTransport  string   `json:"active_transport,omitempty"`
	Enabled          bool     `json:"enabled"`
	Connected        bool     `json:"connected"`
	Endpoint         string   `json:"endpoint"`
	Command          string   `json:"command,omitempty"`
	ArgumentCount    int      `json:"argument_count,omitempty"`
	URL              string   `json:"url,omitempty"`
	WorkingDirectory string   `json:"working_directory,omitempty"`
	EnvironmentKeys  []string `json:"environment_keys,omitempty"`
	HeaderKeys       []string `json:"header_keys,omitempty"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	ToolCount        int      `json:"tool_count"`
	ServerName       string   `json:"server_name,omitempty"`
	ServerVersion    string   `json:"server_version,omitempty"`
	LastError        string   `json:"last_error,omitempty"`
}

type externalMCPConnection struct {
	client          *mcpclient.Client
	activeTransport string
	publicTools     []string
	toolCount       int
	serverName      string
	serverVersion   string
	lastError       string
}

type externalMCPManager struct {
	store *configStore

	serverMu sync.RWMutex
	server   *server.MCPServer

	connectMu   sync.Mutex
	mu          sync.RWMutex
	connections map[string]*externalMCPConnection
}

func newExternalMCPManager(store *configStore) *externalMCPManager {
	return &externalMCPManager{
		store:       store,
		connections: make(map[string]*externalMCPConnection),
	}
}

func (m *externalMCPManager) attachServer(mcpServer *server.MCPServer) {
	m.serverMu.Lock()
	m.server = mcpServer
	m.serverMu.Unlock()
}

func (m *externalMCPManager) startConfigured(ctx context.Context) []error {
	configs := m.store.snapshot().MCPServers
	var failures []error
	for _, config := range configs {
		if !config.Enabled {
			continue
		}
		if _, err := m.connect(ctx, config.ID); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", config.Name, err))
		}
	}
	return failures
}

func (m *externalMCPManager) summary() map[string]any {
	statuses := m.statuses()
	connected := 0
	tools := 0
	for _, status := range statuses {
		if status.Connected {
			connected++
		}
		tools += status.ToolCount
	}
	return map[string]any{
		"configured": len(statuses),
		"connected":  connected,
		"tools":      tools,
	}
}

func (m *externalMCPManager) statuses() []ExternalMCPStatus {
	configs := m.store.snapshot().MCPServers
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]ExternalMCPStatus, 0, len(configs))
	for _, config := range configs {
		connection := m.connections[config.ID]
		status := statusFromExternalMCPConfig(config)
		if connection != nil {
			status.Connected = connection.client != nil
			status.ActiveTransport = connection.activeTransport
			status.ToolCount = connection.toolCount
			status.ServerName = connection.serverName
			status.ServerVersion = connection.serverVersion
			status.LastError = connection.lastError
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return strings.ToLower(statuses[i].Name) < strings.ToLower(statuses[j].Name)
	})
	return statuses
}

func statusFromExternalMCPConfig(config ExternalMCPConfig) ExternalMCPStatus {
	return ExternalMCPStatus{
		ID:               config.ID,
		Name:             config.Name,
		Prefix:           config.Prefix,
		Transport:        config.Transport,
		Enabled:          config.Enabled,
		Endpoint:         externalMCPEndpoint(config),
		Command:          config.Command,
		ArgumentCount:    len(config.Args),
		URL:              config.URL,
		WorkingDirectory: config.WorkingDirectory,
		EnvironmentKeys:  sortedMapKeys(config.Environment),
		HeaderKeys:       sortedMapKeys(config.Headers),
		TimeoutSeconds:   config.TimeoutSeconds,
	}
}

func externalMCPEndpoint(config ExternalMCPConfig) string {
	if isExternalMCPURLTransport(config.Transport) {
		parsed, err := url.Parse(config.URL)
		if err != nil {
			return config.URL
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return config.Command
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *externalMCPManager) config(id string) (ExternalMCPConfig, bool) {
	id = strings.TrimSpace(id)
	for _, config := range m.store.snapshot().MCPServers {
		if config.ID == id {
			return config, true
		}
	}
	return ExternalMCPConfig{}, false
}

func (m *externalMCPManager) addConfig(config ExternalMCPConfig) (ExternalMCPStatus, error) {
	if strings.TrimSpace(config.ID) == "" {
		config.ID = "mcp_" + randomHex(6)
	}
	if err := m.store.update(func(current *TautlineConfig) error {
		current.MCPServers = append(current.MCPServers, config)
		return nil
	}); err != nil {
		return ExternalMCPStatus{}, err
	}
	stored, _ := m.config(config.ID)
	if stored.Enabled {
		status, _ := m.connect(context.Background(), stored.ID)
		return status, nil
	}
	return statusFromExternalMCPConfig(stored), nil
}

func (m *externalMCPManager) updateConfig(config ExternalMCPConfig) (ExternalMCPStatus, error) {
	candidate := m.store.snapshot().MCPServers
	found := false
	for index := range candidate {
		if candidate[index].ID == config.ID {
			candidate[index] = config
			found = true
			break
		}
	}
	if !found {
		return ExternalMCPStatus{}, fmt.Errorf("unknown MCP server %q", config.ID)
	}
	if err := normalizeExternalMCPConfigs(&candidate); err != nil {
		return ExternalMCPStatus{}, err
	}
	m.disconnect(config.ID, true)
	if err := m.store.update(func(current *TautlineConfig) error {
		current.MCPServers = candidate
		return nil
	}); err != nil {
		return ExternalMCPStatus{}, err
	}
	stored, _ := m.config(config.ID)
	if stored.Enabled {
		status, _ := m.connect(context.Background(), stored.ID)
		return status, nil
	}
	return statusFromExternalMCPConfig(stored), nil
}

func (m *externalMCPManager) removeConfig(id string) error {
	if _, exists := m.config(id); !exists {
		return fmt.Errorf("unknown MCP server %q", id)
	}
	m.disconnect(id, true)
	return m.store.update(func(current *TautlineConfig) error {
		filtered := current.MCPServers[:0]
		for _, config := range current.MCPServers {
			if config.ID != id {
				filtered = append(filtered, config)
			}
		}
		current.MCPServers = filtered
		return nil
	})
}

func (m *externalMCPManager) setEnabled(ctx context.Context, id string, enabled bool) (ExternalMCPStatus, error) {
	config, exists := m.config(id)
	if !exists {
		return ExternalMCPStatus{}, fmt.Errorf("unknown MCP server %q", id)
	}
	config.Enabled = enabled
	if err := m.store.update(func(current *TautlineConfig) error {
		for index := range current.MCPServers {
			if current.MCPServers[index].ID == id {
				current.MCPServers[index].Enabled = enabled
				return nil
			}
		}
		return fmt.Errorf("unknown MCP server %q", id)
	}); err != nil {
		return ExternalMCPStatus{}, err
	}
	if enabled {
		return m.connect(ctx, id)
	}
	m.disconnect(id, true)
	return statusFromExternalMCPConfig(config), nil
}

func (m *externalMCPManager) connect(ctx context.Context, id string) (ExternalMCPStatus, error) {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	config, exists := m.config(id)
	if !exists {
		return ExternalMCPStatus{}, fmt.Errorf("unknown MCP server %q", id)
	}
	if !config.Enabled {
		return statusFromExternalMCPConfig(config), errors.New("MCP server is disabled")
	}
	mcpServer := m.attachedServer()
	if mcpServer == nil {
		return ExternalMCPStatus{}, errors.New("Tautline MCP server is not ready")
	}

	client, serverInfo, nativeTools, err := m.openClient(ctx, config)
	if err != nil {
		m.setConnectionError(config.ID, err)
		return m.statusFor(config.ID), err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = client.Close()
		}
	}()

	publicTools, entries, err := buildExternalMCPTools(config, nativeTools, func(callContext context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return m.callTool(callContext, config.ID, request)
	})
	if err != nil {
		m.setConnectionError(config.ID, err)
		return m.statusFor(config.ID), err
	}

	previous := m.takeConnection(config.ID)
	if previous != nil {
		if len(previous.publicTools) > 0 {
			mcpServer.DeleteTools(previous.publicTools...)
		}
		if previous.client != nil {
			_ = previous.client.Close()
		}
	}
	for _, name := range publicTools {
		if mcpServer.GetTool(name) != nil {
			err = fmt.Errorf("public tool name %q is already registered", name)
			m.setConnectionError(config.ID, err)
			return m.statusFor(config.ID), err
		}
	}

	client.OnConnectionLost(func(connectionErr error) {
		m.markConnectionLost(config.ID, client, connectionErr)
	})
	m.mu.Lock()
	m.connections[config.ID] = &externalMCPConnection{
		client:          client,
		activeTransport: externalMCPClientTransportName(client),
		publicTools:     publicTools,
		toolCount:       len(publicTools),
		serverName:      serverInfo.ServerInfo.Name,
		serverVersion:   serverInfo.ServerInfo.Version,
	}
	m.mu.Unlock()
	mcpServer.AddTools(entries...)
	closeOnError = false
	return m.statusFor(config.ID), nil
}

func (m *externalMCPManager) openClient(ctx context.Context, config ExternalMCPConfig) (*mcpclient.Client, *mcp.InitializeResult, []mcp.Tool, error) {
	transportName := normalizeExternalMCPTransport(config.Transport)
	if transportName == externalMCPTransportAuto {
		return m.openClientAutomatic(ctx, config)
	}
	return m.openClientUsingTransport(ctx, config, transportName)
}

func (m *externalMCPManager) externalMCPWorkingDirectory(config ExternalMCPConfig) (string, error) {
	if strings.TrimSpace(config.WorkingDirectory) != "" {
		directory, err := resolvePath(config.WorkingDirectory)
		if err != nil {
			return "", fmt.Errorf("working directory: %w", err)
		}
		info, err := os.Stat(directory)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("working directory is not a directory: %s", config.WorkingDirectory)
		}
		return directory, nil
	}
	directory := filepath.Join(m.store.snapshot().RuntimeDir, "mcp", config.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	return directory, nil
}

func (m *externalMCPManager) callTool(ctx context.Context, id string, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := m.clientForCall(ctx, id)
	if err != nil {
		return nil, err
	}
	result, callErr := client.CallTool(ctx, request)
	if callErr == nil {
		return result, nil
	}
	m.markConnectionLost(id, client, callErr)
	client, err = m.clientForCall(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("MCP call %s failed and reconnect failed: %w", request.Params.Name, err)
	}
	result, callErr = client.CallTool(ctx, request)
	if callErr != nil {
		m.markConnectionLost(id, client, callErr)
		return nil, fmt.Errorf("MCP call %s failed: %w", request.Params.Name, callErr)
	}
	return result, nil
}

func (m *externalMCPManager) clientForCall(ctx context.Context, id string) (*mcpclient.Client, error) {
	m.mu.RLock()
	connection := m.connections[id]
	var client *mcpclient.Client
	if connection != nil {
		client = connection.client
	}
	m.mu.RUnlock()
	if client != nil {
		return client, nil
	}
	if _, err := m.connect(ctx, id); err != nil {
		return nil, err
	}
	m.mu.RLock()
	connection = m.connections[id]
	if connection != nil {
		client = connection.client
	}
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("MCP client was not created")
	}
	return client, nil
}

func (m *externalMCPManager) disconnect(id string, removeTools bool) {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()
	mcpServer := m.attachedServer()
	connection := m.takeConnection(id)
	if connection == nil {
		return
	}
	if removeTools && mcpServer != nil && len(connection.publicTools) > 0 {
		mcpServer.DeleteTools(connection.publicTools...)
	}
	if connection.client != nil {
		_ = connection.client.Close()
	}
}

func (m *externalMCPManager) closeAll() {
	configs := m.store.snapshot().MCPServers
	for _, config := range configs {
		m.disconnect(config.ID, true)
	}
}

func (m *externalMCPManager) attachedServer() *server.MCPServer {
	m.serverMu.RLock()
	defer m.serverMu.RUnlock()
	return m.server
}

func (m *externalMCPManager) takeConnection(id string) *externalMCPConnection {
	m.mu.Lock()
	defer m.mu.Unlock()
	connection := m.connections[id]
	delete(m.connections, id)
	return connection
}

func (m *externalMCPManager) markConnectionLost(id string, client *mcpclient.Client, cause error) {
	m.mu.Lock()
	connection := m.connections[id]
	if connection == nil || connection.client != client {
		m.mu.Unlock()
		return
	}
	connection.client = nil
	if cause != nil {
		connection.lastError = cause.Error()
	} else {
		connection.lastError = "connection closed"
	}
	m.mu.Unlock()
	_ = client.Close()
}

func (m *externalMCPManager) setConnectionError(id string, cause error) {
	m.mu.Lock()
	connection := m.connections[id]
	if connection == nil {
		connection = &externalMCPConnection{}
		m.connections[id] = connection
	}
	if cause == nil {
		connection.lastError = ""
	} else {
		connection.lastError = cause.Error()
	}
	m.mu.Unlock()
}

func (m *externalMCPManager) statusFor(id string) ExternalMCPStatus {
	for _, status := range m.statuses() {
		if status.ID == id {
			return status
		}
	}
	return ExternalMCPStatus{ID: id}
}

func buildExternalMCPTools(config ExternalMCPConfig, nativeTools []mcp.Tool, caller func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) ([]string, []server.ServerTool, error) {
	if caller == nil {
		return nil, nil, errors.New("MCP tool caller is nil")
	}
	seen := make(map[string]string, len(nativeTools))
	publicNames := make([]string, 0, len(nativeTools))
	entries := make([]server.ServerTool, 0, len(nativeTools))
	for _, nativeTool := range nativeTools {
		nativeName := strings.TrimSpace(nativeTool.Name)
		if nativeName == "" {
			continue
		}
		publicName := publicExternalToolName(config.Prefix, nativeName)
		if previous, exists := seen[publicName]; exists {
			return nil, nil, fmt.Errorf("MCP tools %q and %q map to the same public name %q", previous, nativeName, publicName)
		}
		seen[publicName] = nativeName

		schema := nativeTool.RawInputSchema
		if len(schema) == 0 {
			encoded, err := json.Marshal(nativeTool.InputSchema)
			if err != nil {
				return nil, nil, fmt.Errorf("encode MCP tool schema %s: %w", nativeName, err)
			}
			schema = encoded
		}
		description := strings.TrimSpace(nativeTool.Description)
		if description == "" {
			description = fmt.Sprintf("Call %s tool %s.", config.Name, nativeName)
		} else {
			description = config.Name + ": " + description
		}
		tool := mcp.NewToolWithRawSchema(publicName, description, schema)
		tool.Annotations = nativeTool.Annotations
		capturedNativeName := nativeName
		entries = append(entries, server.ServerTool{
			Tool: tool,
			Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				request.Params.Name = capturedNativeName
				return caller(ctx, request)
			},
		})
		publicNames = append(publicNames, publicName)
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("MCP server returned no usable tools")
	}
	return publicNames, entries, nil
}

func publicExternalToolName(prefix, nativeName string) string {
	return normalizeMCPToken(prefix) + "_" + normalizeMCPToken(nativeName)
}

func normalizeMCPToken(value string) string {
	var builder strings.Builder
	input := []rune(strings.TrimSpace(value))
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
	return strings.Trim(builder.String(), "_")
}

func normalizeExternalMCPConfigs(configs *[]ExternalMCPConfig) error {
	seenIDs := make(map[string]bool, len(*configs))
	seenPrefixes := make(map[string]bool, len(*configs))
	for index := range *configs {
		config := &(*configs)[index]
		config.ID = normalizeMCPToken(config.ID)
		config.Name = strings.TrimSpace(config.Name)
		if config.Name == "" {
			return fmt.Errorf("MCP server %d has no name", index+1)
		}
		if config.ID == "" {
			config.ID = "mcp_" + normalizeMCPToken(config.Name)
		}
		if seenIDs[config.ID] {
			return fmt.Errorf("duplicate MCP server id %q", config.ID)
		}
		seenIDs[config.ID] = true

		config.Prefix = normalizeMCPToken(config.Prefix)
		if config.Prefix == "" {
			config.Prefix = normalizeMCPToken(config.Name)
		}
		if config.Prefix == "" {
			return fmt.Errorf("MCP server %q has an invalid tool prefix", config.Name)
		}
		if seenPrefixes[config.Prefix] {
			return fmt.Errorf("duplicate MCP tool prefix %q", config.Prefix)
		}
		seenPrefixes[config.Prefix] = true

		config.Transport = normalizeExternalMCPTransport(config.Transport)
		config.Command = strings.TrimSpace(config.Command)
		config.WorkingDirectory = strings.TrimSpace(config.WorkingDirectory)
		config.URL = normalizeExternalMCPURL(strings.TrimSpace(config.URL))
		config.Args = compactStrings(config.Args)
		config.Environment = normalizeStringMap(config.Environment)
		config.Headers = normalizeStringMap(config.Headers)
		if config.OAuth != nil {
			config.OAuth.ClientID = strings.TrimSpace(config.OAuth.ClientID)
			config.OAuth.ClientSecret = strings.TrimSpace(config.OAuth.ClientSecret)
			config.OAuth.RedirectURI = strings.TrimSpace(config.OAuth.RedirectURI)
			config.OAuth.TokenFile = strings.TrimSpace(config.OAuth.TokenFile)
			config.OAuth.AuthServerMetadataURL = strings.TrimSpace(config.OAuth.AuthServerMetadataURL)
			config.OAuth.Scopes = compactStrings(config.OAuth.Scopes)
			config.OAuth.AuthorizationParams = normalizeStringMap(config.OAuth.AuthorizationParams)
		}
		if config.TimeoutSeconds == 0 {
			config.TimeoutSeconds = defaultExternalMCPTimeout
		}
		if config.TimeoutSeconds < 5 || config.TimeoutSeconds > 300 {
			return fmt.Errorf("MCP server %q timeout must be between 5 and 300 seconds", config.Name)
		}
		switch config.Transport {
		case externalMCPTransportStdio:
			if config.Command == "" {
				return fmt.Errorf("MCP server %q requires a command", config.Name)
			}
			if config.OAuth != nil {
				return fmt.Errorf("MCP server %q can use OAuth only with a URL transport", config.Name)
			}
		case externalMCPTransportAuto, externalMCPTransportStreamableHTTP, externalMCPTransportSSE:
			parsed, err := url.Parse(config.URL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("MCP server %q has an invalid HTTP URL", config.Name)
			}
			if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("MCP server %q URL must not contain credentials, query parameters, or fragments", config.Name)
			}
			if parsed.Scheme == "http" && !isLoopbackMCPHost(parsed.Hostname()) {
				return fmt.Errorf("MCP server %q must use HTTPS unless it runs on localhost", config.Name)
			}
			if config.OAuth != nil {
				if err := validateExternalMCPOAuthConfig(config.Name, config.OAuth); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("MCP server %q has unsupported transport %q", config.Name, config.Transport)
		}
	}
	return nil
}

func normalizeExternalMCPURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return rawURL
	}
	if parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "docsmcp.googleapis.com") && (parsed.EscapedPath() == "/mcp" || parsed.EscapedPath() == "/mcp/") {
		parsed.Path = "/mcp/v1"
		parsed.RawPath = ""
		return parsed.String()
	}
	return rawURL
}

func isLoopbackMCPHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func cloneExternalMCPConfigs(configs []ExternalMCPConfig) []ExternalMCPConfig {
	cloned := make([]ExternalMCPConfig, len(configs))
	for index, config := range configs {
		cloned[index] = config
		cloned[index].Args = append([]string(nil), config.Args...)
		cloned[index].Environment = cloneStringMap(config.Environment)
		cloned[index].Headers = cloneStringMap(config.Headers)
		if config.OAuth != nil {
			oauthCopy := *config.OAuth
			oauthCopy.Scopes = append([]string(nil), config.OAuth.Scopes...)
			oauthCopy.AuthorizationParams = cloneStringMap(config.OAuth.AuthorizationParams)
			cloned[index].OAuth = &oauthCopy
		}
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			normalized[key] = value
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func compactStrings(values []string) []string {
	compacted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			compacted = append(compacted, value)
		}
	}
	return compacted
}

func environmentEntries(values map[string]string) []string {
	keys := sortedMapKeys(values)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, key+"="+values[key])
	}
	return entries
}

func resolveExternalMCPValues(values map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(values))
	for key, value := range values {
		missing := ""
		resolvedValue := os.Expand(value, func(name string) string {
			if current, exists := os.LookupEnv(name); exists {
				return current
			}
			missing = name
			return ""
		})
		if missing != "" {
			return nil, fmt.Errorf("MCP setting %q references missing environment variable %s", key, missing)
		}
		resolved[key] = resolvedValue
	}
	return resolved, nil
}

func externalMCPChildEnvironment(extra []string) []string {
	allowed := map[string]bool{
		"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true,
		"TEMP": true, "TMP": true, "TMPDIR": true, "HOME": true, "USER": true, "USERNAME": true,
		"USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "PROGRAMDATA": true,
		"LANG": true, "LC_ALL": true, "TERM": true, "SHELL": true,
	}
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 && allowed[strings.ToUpper(parts[0])] {
			values[parts[0]] = parts[1]
		}
	}
	for _, entry := range extra {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			values[strings.TrimSpace(parts[0])] = parts[1]
		}
	}
	return environmentEntries(values)
}
