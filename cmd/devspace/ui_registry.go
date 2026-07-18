package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	widgetMIMEType     = "text/html;profile=mcp-app"
	workspaceWidgetURI = "ui://devspace/workspace-card-v3.html"
	fileWidgetURI      = "ui://devspace/file-viewer-v2.html"
	diffWidgetURI      = "ui://devspace/diff-viewer-v2.html"
	commandWidgetURI   = "ui://devspace/command-result-v2.html"
	changesWidgetURI   = "ui://devspace/changes-review-v1.html"
)

type widgetDefinition struct {
	URI         string
	Name        string
	Description string
	HTML        string
}

func registerWidgetResource(s *server.MCPServer) {
	if activeWidgetMode == widgetModeOff {
		return
	}
	widgets := []widgetDefinition{
		{
			URI:         workspaceWidgetURI,
			Name:        "DevSpace workspace",
			Description: "A clean workspace card shown once when a coding session opens.",
			HTML:        workspaceWidgetHTML(),
		},
	}
	if activeWidgetMode == widgetModeChanges {
		widgets = append(widgets, widgetDefinition{
			URI:         changesWidgetURI,
			Name:        "DevSpace change review",
			Description: "One aggregate change review rendered after the final edit in a coding turn.",
			HTML:        changesWidgetHTML(),
		})
	}
	if activeWidgetMode == widgetModeFull {
		widgets = append(widgets,
			widgetDefinition{
				URI:         fileWidgetURI,
				Name:        "DevSpace file viewer",
				Description: "A compact UTF-8 file preview that expands to a full file view.",
				HTML:        fileWidgetHTML(),
			},
			widgetDefinition{
				URI:         diffWidgetURI,
				Name:        "DevSpace diff viewer",
				Description: "A focused unified-diff card for individual write and edit operations.",
				HTML:        diffWidgetHTML(),
			},
			widgetDefinition{
				URI:         commandWidgetURI,
				Name:        "DevSpace command result",
				Description: "A command card with exit status, duration, directory, and bounded output.",
				HTML:        commandWidgetHTML(),
			},
		)
	}

	for _, item := range widgets {
		item := item
		resource := mcp.NewResource(
			item.URI,
			item.Name,
			mcp.WithResourceDescription(item.Description),
			mcp.WithMIMEType(widgetMIMEType),
		)
		resource.Meta = widgetResourceMeta(item.Description)

		s.AddResource(resource, func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      item.URI,
					MIMEType: widgetMIMEType,
					Text:     item.HTML,
					Meta:     widgetResourceMetaMap(item.Description),
				},
			}, nil
		})
	}
}

func widgetResourceMeta(description string) *mcp.Meta {
	return mcp.NewMetaFromMap(widgetResourceMetaMap(description))
}

func widgetResourceMetaMap(description string) map[string]any {
	return map[string]any{
		"ui": map[string]any{
			"prefersBorder": true,
			"csp": map[string]any{
				"connectDomains":  []string{},
				"resourceDomains": []string{},
			},
		},
		"openai/widgetDescription":   description,
		"openai/widgetPrefersBorder": true,
		"openai/widgetCSP": map[string]any{
			"connect_domains":  []string{},
			"resource_domains": []string{},
		},
	}
}
