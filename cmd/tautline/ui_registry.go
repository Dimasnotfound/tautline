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
	activityWidgetURI = "ui://tautline/activity-v6.html"
)

type widgetResourceDefinition struct {
	URI         string
	Name        string
	Description string
}

func widgetResourceDefinitions() []widgetResourceDefinition {
	return []widgetResourceDefinition{{
		URI:         activityWidgetURI,
		Name:        "Tautline activity monitor",
		Description: "One prompt-scoped Tautline monitor for workspace activity, file changes, commands, agents, browser actions, skills, and connected MCP tools.",
	}}
}

func registerWidgetResource(s *server.MCPServer) {
	if activeWidgetMode == widgetModeOff {
		return
	}
	definition := widgetResourceDefinitions()[0]
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
				Text:     activityWidgetHTML(),
				Meta:     widgetResourceMetaMap(definition.Description),
			},
		}, nil
	})
}

func widgetResourceMeta(description string) *mcp.Meta {
	return mcp.NewMetaFromMap(widgetResourceMetaMap(description))
}

func widgetResourceMetaMap(description string) map[string]any {
	uiMeta := map[string]any{
		"prefersBorder": false,
		"csp": map[string]any{
			"connectDomains":  []string{},
			"resourceDomains": []string{},
		},
	}
	meta := map[string]any{
		"ui":                         uiMeta,
		"openai/widgetDescription":   description,
		"openai/widgetPrefersBorder": false,
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
