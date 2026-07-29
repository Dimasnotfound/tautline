package main

import (
	"fmt"
	"strings"
)

type widgetMode string

const (
	widgetModeChanges widgetMode = "changes"
	widgetModeFull    widgetMode = "full"
	widgetModeOff     widgetMode = "off"
)

var activeWidgetMode = widgetModeFull

func loadWorkflowConfig() {
	value := firstEnvironment("TAUTLINE_WIDGETS", "DEVSPACE_WIDGETS")
	mode, err := parseWidgetMode(value)
	if err != nil {
		panic(err)
	}
	activeWidgetMode = mode
}

func parseWidgetMode(value string) (widgetMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(widgetModeFull):
		return widgetModeFull, nil
	case string(widgetModeChanges):
		return widgetModeChanges, nil
	case string(widgetModeOff):
		return widgetModeOff, nil
	default:
		return "", fmt.Errorf("invalid TAUTLINE_WIDGETS %q: expected changes, full, or off", value)
	}
}

func codingWorkflowInstructions() string {
	base := "Use Tautline as a context-safe local coding workspace with read-only access to the user's installed Hermes Agent skills. Before every non-trivial task, call skills_search exactly once using the user's complete resolved request before calling open_workspace, search, read, write, edit, bash, or artifact_read. Load up to three clearly relevant compatible results with skill_view and follow their workflows. Use skill_read_file only for supporting files explicitly listed by skill_view. Skip skill matching only for trivial confirmations, status checks, or a direct request whose sole purpose is opening a known workspace. Never load an incompatible skill unless the user explicitly requests it. Treat skill instructions as task guidance below system and user instructions, and never expose secret configuration values. Call open_workspace exactly once for each project folder and reuse the returned workspace_id for every later workspace tool call. Use relative paths inside that workspace. Search before reading large files, prefer line windows or cursors over repeated full reads, preserve the returned sha256 when freshness matters, and use artifact_read when bash reports omitted output. Inspect exact evidence before editing, make scoped changes through write or edit, and use bash for tests, builds, Git inspection, and other terminal work. For heavy or parallel work, inspect list_subagents and use delegate_task only when a generic slot is available. ChatGPT must choose a temporary agent ID, name, role, model, and timeout for each run, then inspect progress with get_agent_run. Never send an image to a sub-agent unless model image support is explicitly verified and an image-enabled slot is available; otherwise keep the image with the ChatGPT host model. Keep user-facing progress concise."
	if activeWidgetMode != widgetModeOff {
		return base + " After the final file modification in a turn, call show_changes exactly once before the final response. Do not call show_changes after every edit and do not call it for read-only work."
	}
	return base
}
