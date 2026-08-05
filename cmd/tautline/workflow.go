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
	base := "Use Tautline as a context-safe local coding workspace with read-only access to the user's installed Hermes Agent skills. Before every non-trivial task, call skills_search exactly once using the user's complete resolved request before calling workspace_lookup, open_workspace, search, read, read_many, write, edit, bash, exec_command, write_stdin, or artifact_read. Load up to three clearly relevant compatible results with skill_view and follow their workflows. Use skill_read_file only for supporting files explicitly listed by skill_view. Skip skill matching only for trivial confirmations, status checks, or a direct request whose sole purpose is opening a known workspace. Never load an incompatible skill unless the user explicitly requests it. Treat skill instructions as task guidance below system and user instructions, and never expose secret configuration values. For the user's existing checkout, call workspace_lookup with the absolute project path and reuse its workspace_id when found. Call open_workspace in checkout mode only when lookup reports that the project is not open. When isolated or parallel Git work is requested, call open_workspace with mode=worktree and an optional base_ref; every worktree call intentionally creates a new detached managed workspace, so do not reuse the checkout workspace_id for it. Reuse each returned workspace_id for later calls in that specific checkout or worktree and use relative paths inside it. Search before reading large files, prefer line windows or cursors over repeated full reads, use read_many only for a small known set of up to ten bounded file requests, preserve the returned sha256 when freshness matters, and use artifact_read when bash reports omitted output. Use tautline_doctor for read-only runtime diagnosis when connection, configuration, integration, or version state is uncertain. Inspect exact evidence before editing and make scoped changes through write or edit. Use bash for bounded one-shot tests, builds, Git inspection, and terminal work. Use exec_command for commands that may remain running or require later input, then use write_stdin with the returned process session ID to poll output, send input, interrupt, or terminate it. For heavy or parallel work, inspect list_subagents and use delegate_task only when a generic slot is available. With the default chatgpt-relay backend, delegate_task queues worker_prompt for the optional Laju Relay Bridge, which may open a fresh ordinary ChatGPT worker tab and submit it through the visible ChatGPT composer; when the bridge is disconnected, tell the user to open a new ordinary ChatGPT conversation and paste worker_prompt manually. Tautline never calls a private consumer ChatGPT backend or uses a ChatGPT account token. When a user supplies a relay join code in a worker chat, call claim_agent_task, keep worker_token private, perform only the returned task, use update_agent_run only for meaningful progress, and call complete_agent_task exactly once before the final worker response. For the optional 9router backend, ChatGPT must choose a temporary agent ID, name, role, model, and timeout, then inspect progress with get_agent_run. Never send an image to a relay agent automatically; the user must attach it manually in the worker chat. Keep user-facing progress concise."
	if activeWidgetMode != widgetModeOff {
		return "For every non-trivial user turn that uses Tautline or MyLocal, call skills_search exactly once before any other Tautline tool. skills_search creates and renders the new prompt-scoped activity monitor, so do not also call tautline_activity in that turn. For a trivial status check or direct workspace request that legitimately skips skill matching, call tautline_activity exactly once before any other Tautline tool. Both prompt-boundary tools archive the previous monitor so old widgets cannot receive later activity; all remaining workspace, skill, command, agent, browser, and external MCP tools are data-only. " + base + " After the final file modification in a turn, call show_changes exactly once before the final response. Do not call show_changes after every edit and do not call it for read-only work."
	}
	return base
}
