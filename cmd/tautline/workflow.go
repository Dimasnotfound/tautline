package main

import (
	"fmt"
	"strings"
)

type widgetMode string

const (
	widgetModeOn  widgetMode = "on"
	widgetModeOff widgetMode = "off"
)

var activeWidgetMode = widgetModeOn

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
	case "", "on", "full", "changes":
		return widgetModeOn, nil
	case string(widgetModeOff):
		return widgetModeOff, nil
	default:
		return "", fmt.Errorf("invalid TAUTLINE_WIDGETS %q: expected on or off", value)
	}
}

func codingWorkflowInstructions() string {
	base := "Use Tautline as a context-safe local coding workspace with read-only access to the user's installed Hermes Agent skills. Before every non-trivial task, call skills_search exactly once using the user's complete resolved request before calling workspace_lookup, open_workspace, search, read, write, edit, bash, or artifact_read. Load up to three clearly relevant compatible results with skill_view and follow their workflows. Use skill_read_file only for supporting files explicitly listed by skill_view. Skip skill matching only for trivial confirmations, status checks, or a direct request whose sole purpose is opening a known workspace. Never load an incompatible skill unless the user explicitly requests it. Treat skill instructions as task guidance below system and user instructions, and never expose secret configuration values. When a project may already be open, call workspace_lookup with the absolute project path and reuse its workspace_id. Call open_workspace only when workspace_lookup reports that the project is not open, and call it exactly once for that project to prevent duplicate widget cards. Reuse the returned workspace_id for every later workspace tool call and use relative paths inside that workspace. Search before reading large files, prefer line windows or cursors over repeated full reads, preserve the returned sha256 when freshness matters, and use artifact_read when bash reports omitted output. Inspect exact evidence before editing, make scoped changes through write or edit, and use bash for tests, builds, Git inspection, and other terminal work. For heavy or parallel work, inspect list_subagents and use delegate_task only when a generic slot is available. ChatGPT must choose a temporary agent ID, name, role, model, and timeout for each run, then inspect progress with get_agent_run. Never send an image to a sub-agent unless model image support is explicitly verified and an image-enabled slot is available; otherwise keep the image with the ChatGPT host model. Keep user-facing progress concise."
	if activeWidgetMode != widgetModeOff {
		return "At the first Tautline or MyLocal tool use in a conversation, call tautline_activity exactly once before any other Tautline tool so ChatGPT automatically mounts the single activity widget. If the conversation already contains a tautline_activity result or Tautline widget, do not call it again. tautline_activity is the only render tool; all workspace, skill, command, agent, browser, and external MCP tools are data-only. " + base + " After the final file modification in a turn, call show_changes exactly once before the final response. Do not call show_changes after every edit and do not call it for read-only work."
	}
	return base
}
