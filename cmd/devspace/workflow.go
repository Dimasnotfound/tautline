package main

import (
	"fmt"
	"os"
	"strings"
)

type widgetMode string

const (
	widgetModeChanges widgetMode = "changes"
	widgetModeFull    widgetMode = "full"
	widgetModeOff     widgetMode = "off"
)

var activeWidgetMode = widgetModeChanges

func loadWorkflowConfig() {
	mode, err := parseWidgetMode(os.Getenv("DEVSPACE_WIDGETS"))
	if err != nil {
		panic(err)
	}
	activeWidgetMode = mode
}

func parseWidgetMode(value string) (widgetMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(widgetModeChanges):
		return widgetModeChanges, nil
	case string(widgetModeFull):
		return widgetModeFull, nil
	case string(widgetModeOff):
		return widgetModeOff, nil
	default:
		return "", fmt.Errorf("invalid DEVSPACE_WIDGETS %q: expected changes, full, or off", value)
	}
}

func codingWorkflowInstructions() string {
	base := "Use DevSpace as a local coding workspace. Call open_workspace exactly once for each project folder and reuse the returned workspace_id for every later tool call. Use relative paths inside that workspace. Inspect relevant files before editing, make scoped changes through write or edit, and use bash for tests, builds, Git inspection, and other terminal work. Keep user-facing progress concise."
	if activeWidgetMode == widgetModeChanges {
		return base + " After the final file modification in a turn, call show_changes exactly once before the final response. Do not call show_changes after every edit and do not call it for read-only work."
	}
	return base
}
