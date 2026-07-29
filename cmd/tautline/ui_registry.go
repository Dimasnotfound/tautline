package main

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	widgetMIMEType    = "text/html;profile=mcp-app"
	toolCardWidgetURI = "ui://tautline/tool-card-v2.html"

	workspaceWidgetURI = toolCardWidgetURI
	fileWidgetURI      = toolCardWidgetURI
	diffWidgetURI      = toolCardWidgetURI
	commandWidgetURI   = toolCardWidgetURI
	changesWidgetURI   = toolCardWidgetURI
)

type widgetResourceDefinition struct {
	URI         string
	Name        string
	Description string
}

func widgetResourceDefinitions() []widgetResourceDefinition {
	return []widgetResourceDefinition{
		{
			URI:         toolCardWidgetURI,
			Name:        "Tautline tool and agent card",
			Description: "A compact Tautline card for workspace tools, command evidence, browser results, and live sub-agent activity.",
		},
		{
			URI:         "ui://tautline/tool-card-v1.html",
			Name:        "Tautline v2 preview compatibility alias",
			Description: "Compatibility alias for early Tautline v2 tool cards.",
		},
		{
			URI:         "ui://devspace/tool-card-v5.html",
			Name:        "DevSpace v1.8 compatibility alias",
			Description: "Compatibility alias for conversations cached from DevSpace v1.8.0.",
		},
		{
			URI:         "ui://devspace/tool-card-v4.html",
			Name:        "DevSpace v1.7 compatibility alias",
			Description: "Compatibility alias for conversations cached from DevSpace v1.7.0.",
		},
		{
			URI:         "ui://devspace/tool-card-v3.html",
			Name:        "DevSpace legacy compatibility alias",
			Description: "Compatibility alias for older DevSpace tool cards.",
		},
		{
			URI:         "ui://devspace/tool-card-v2.html",
			Name:        "DevSpace legacy compatibility alias",
			Description: "Compatibility alias for older DevSpace tool cards.",
		},
		{
			URI:         "ui://devspace/tool-card-v1.html",
			Name:        "DevSpace legacy compatibility alias",
			Description: "Compatibility alias for older DevSpace tool cards.",
		},
		{
			URI:         "ui://devspace/workspace-card-v3.html",
			Name:        "DevSpace workspace compatibility alias",
			Description: "Compatibility alias for older DevSpace workspace cards.",
		},
		{
			URI:         "ui://devspace/workspace-card-v2.html",
			Name:        "DevSpace workspace compatibility alias",
			Description: "Compatibility alias for older DevSpace workspace cards.",
		},
		{
			URI:         "ui://devspace/file-viewer-v2.html",
			Name:        "DevSpace file compatibility alias",
			Description: "Compatibility alias for older DevSpace file cards.",
		},
		{
			URI:         "ui://devspace/diff-viewer-v2.html",
			Name:        "DevSpace diff compatibility alias",
			Description: "Compatibility alias for older DevSpace diff cards.",
		},
		{
			URI:         "ui://devspace/command-result-v2.html",
			Name:        "DevSpace command compatibility alias",
			Description: "Compatibility alias for older DevSpace command cards.",
		},
		{
			URI:         "ui://devspace/changes-review-v1.html",
			Name:        "DevSpace changes compatibility alias",
			Description: "Compatibility alias for older DevSpace change cards.",
		},
	}
}

func registerWidgetResource(s *server.MCPServer) {
	if activeWidgetMode == widgetModeOff {
		return
	}
	for _, definition := range widgetResourceDefinitions() {
		definition := definition
		resource := mcp.NewResource(
			definition.URI,
			definition.Name,
			mcp.WithResourceDescription(definition.Description),
			mcp.WithMIMEType(widgetMIMEType),
		)
		resource.Meta = widgetResourceMeta(definition.Description)
		s.AddResource(resource, func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      definition.URI,
					MIMEType: widgetMIMEType,
					Text:     toolCardWidgetHTML(),
					Meta:     widgetResourceMetaMap(definition.Description),
				},
			}, nil
		})
	}
}

func widgetResourceMeta(description string) *mcp.Meta {
	return mcp.NewMetaFromMap(widgetResourceMetaMap(description))
}

func widgetResourceMetaMap(description string) map[string]any {
	uiMeta := map[string]any{
		"prefersBorder": true,
		"csp": map[string]any{
			"connectDomains":  []string{},
			"resourceDomains": []string{},
		},
	}
	meta := map[string]any{
		"ui":                         uiMeta,
		"openai/widgetDescription":   description,
		"openai/widgetPrefersBorder": true,
		"openai/widgetCSP": map[string]any{
			"connect_domains":  []string{},
			"resource_domains": []string{},
		},
	}
	if domain := widgetDomainOrigin(); domain != "" {
		uiMeta["domain"] = domain
		meta["openai/widgetDomain"] = domain
	}
	return meta
}

func widgetDomainOrigin() string {
	raw := strings.TrimSpace(os.Getenv("TAUTLINE_WIDGET_DOMAIN"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("TAUTLINE_PUBLIC_BASE_URL"))
	}
	if raw == "" {
		if runtime, err := currentApplicationRuntime(); err == nil {
			cfg := runtime.config.snapshot()
			raw = cfg.WidgetDomain
			if raw == "" {
				raw = cfg.PublicBaseURL
			}
		}
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("DEVSPACE_WIDGET_DOMAIN"))
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("DEVSPACE_PUBLIC_BASE_URL"))
	}
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
