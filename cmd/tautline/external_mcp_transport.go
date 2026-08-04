package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	externalMCPTransportStdio          = "stdio"
	externalMCPTransportAuto           = "auto"
	externalMCPTransportStreamableHTTP = "streamable-http"
	externalMCPTransportSSE            = "sse"
)

func normalizeExternalMCPTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return externalMCPTransportStdio
	case "stdio", "process", "command":
		return externalMCPTransportStdio
	case "auto", "automatic":
		return externalMCPTransportAuto
	case "http", "streamable-http", "streamable_http", "streamablehttp":
		return externalMCPTransportStreamableHTTP
	case "sse", "legacy-sse", "legacy_sse", "http+sse":
		return externalMCPTransportSSE
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func isExternalMCPURLTransport(value string) bool {
	switch normalizeExternalMCPTransport(value) {
	case externalMCPTransportAuto, externalMCPTransportStreamableHTTP, externalMCPTransportSSE:
		return true
	default:
		return false
	}
}

func externalMCPClientTransportName(client *mcpclient.Client) string {
	if client == nil {
		return ""
	}
	switch client.GetTransport().(type) {
	case *transport.Stdio:
		return externalMCPTransportStdio
	case *transport.StreamableHTTP:
		return externalMCPTransportStreamableHTTP
	case *transport.SSE:
		return externalMCPTransportSSE
	default:
		return "custom"
	}
}

func (m *externalMCPManager) openClientAutomatic(ctx context.Context, config ExternalMCPConfig) (*mcpclient.Client, *mcp.InitializeResult, []mcp.Tool, error) {
	client, serverInfo, tools, streamableErr := m.openClientUsingTransport(ctx, config, externalMCPTransportStreamableHTTP)
	if streamableErr == nil {
		return client, serverInfo, tools, nil
	}
	if !shouldFallbackExternalMCPToSSE(streamableErr) {
		return nil, nil, nil, streamableErr
	}

	client, serverInfo, tools, sseErr := m.openClientUsingTransport(ctx, config, externalMCPTransportSSE)
	if sseErr == nil {
		return client, serverInfo, tools, nil
	}
	return nil, nil, nil, fmt.Errorf("automatic MCP transport failed: Streamable HTTP: %v; legacy SSE: %w", streamableErr, sseErr)
}

func shouldFallbackExternalMCPToSSE(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, blocker := range []string{
		"unauthorized", "forbidden", "oauth", "access token", "refresh token",
		"x509", "tls", "certificate", "no such host", "connection refused",
		"context canceled", "deadline exceeded",
	} {
		if strings.Contains(message, blocker) {
			return false
		}
	}
	for _, compatibleFailure := range []string{
		"4xx", "404", "405", "not found", "method not allowed", "unexpected content type",
		"legacy sse", "text/event-stream", "endpoint event", "invalid character",
	} {
		if strings.Contains(message, compatibleFailure) {
			return true
		}
	}
	return false
}

func (m *externalMCPManager) openClientUsingTransport(ctx context.Context, config ExternalMCPConfig, selectedTransport string) (*mcpclient.Client, *mcp.InitializeResult, []mcp.Tool, error) {
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	initializeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var client *mcpclient.Client
	var err error
	switch selectedTransport {
	case externalMCPTransportStdio:
		workingDirectory, directoryErr := m.externalMCPWorkingDirectory(config)
		if directoryErr != nil {
			return nil, nil, nil, directoryErr
		}
		resolvedEnvironment, environmentErr := resolveExternalMCPValues(config.Environment)
		if environmentErr != nil {
			return nil, nil, nil, environmentErr
		}
		configuredEnvironment := environmentEntries(resolvedEnvironment)
		commandFactory := transport.WithCommandFunc(func(commandContext context.Context, command string, environment []string, args []string) (*exec.Cmd, error) {
			process := exec.CommandContext(commandContext, command, args...)
			process.Dir = workingDirectory
			process.Env = externalMCPChildEnvironment(environment)
			return process, nil
		})
		client, err = mcpclient.NewStdioMCPClientWithOptions(config.Command, configuredEnvironment, config.Args, commandFactory)
	case externalMCPTransportStreamableHTTP:
		client, err = m.newExternalMCPHTTPClient(config, timeout)
	case externalMCPTransportSSE:
		client, err = m.newExternalMCPSSEClient(config, timeout)
	default:
		err = fmt.Errorf("unsupported MCP transport %q", selectedTransport)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("start %s MCP client: %w", selectedTransport, err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = client.Close()
		}
	}()

	if err := client.Start(initializeContext); err != nil {
		return nil, nil, nil, fmt.Errorf("start %s MCP transport: %w", selectedTransport, err)
	}

	initialize := mcp.InitializeRequest{}
	initialize.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initialize.Params.ClientInfo = mcp.Implementation{Name: appName, Version: appVersion}
	serverInfo, err := client.Initialize(initializeContext, initialize)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize %s MCP server: %w", selectedTransport, err)
	}
	listed, err := client.ListTools(initializeContext, mcp.ListToolsRequest{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list %s MCP tools: %w", selectedTransport, err)
	}
	if len(listed.Tools) == 0 {
		return nil, nil, nil, errors.New("MCP server returned no tools")
	}
	closeOnError = false
	return client, serverInfo, listed.Tools, nil
}
