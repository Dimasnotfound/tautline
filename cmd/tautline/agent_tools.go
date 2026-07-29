package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type agentCapacityView struct {
	Kind          string       `json:"kind"`
	Title         string       `json:"title"`
	Summary       string       `json:"summary"`
	Enabled       bool         `json:"enabled"`
	DefaultModel  string       `json:"default_model"`
	AllowedModels []string     `json:"allowed_models"`
	Slots         []AgentSlot  `json:"slots"`
	Runs          []AgentRun   `json:"runs,omitempty"`
	Router        RouterStatus `json:"router"`
}

type agentRunView struct {
	Kind string   `json:"kind"`
	Run  AgentRun `json:"run"`
}

type browserToolView struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	BrowserFetchResult
}

func registerAgentTools(s *server.MCPServer) {
	listTool := mcp.NewTool("list_subagents",
		mcp.WithTitleAnnotation("List sub-agents"),
		mcp.WithDescription("List generic Tautline sub-agent capacity and recent activity. Slots do not have a fixed identity or role; ChatGPT assigns those details only when delegating a task."),
		mcp.WithOutputSchema[agentCapacityView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	maybeSetWidgetMeta("list_subagents", &listTool, toolCardWidgetURI, "Checking sub-agents", "Sub-agents ready")
	s.AddTool(listTool, handleListSubagents)

	delegateTool := mcp.NewTool("delegate_task",
		mcp.WithTitleAnnotation("Delegate task"),
		mcp.WithDescription("Start one asynchronous 9Router sub-agent task. Delegation must be globally enabled and the requested model must be in the dashboard allowlist returned by list_subagents. When model is omitted, Tautline uses the configured default allowed model. ChatGPT decides the temporary agent ID, name, role, and timeout. Use get_agent_run to inspect progress. Image tasks also require an enabled image slot, explicit model_supports_images=true, and an in-memory data:image URL."),
		mcp.WithString("task", mcp.Required(), mcp.Description("Complete delegated instruction")),
		mcp.WithString("workspace_id", mcp.Description("Optional workspace_id previously returned by open_workspace; enables read-only workspace tools for the sub-agent")),
		mcp.WithString("agent_id", mcp.Description("Temporary logical agent ID chosen by ChatGPT")),
		mcp.WithString("name", mcp.Description("Temporary human-readable name chosen by ChatGPT")),
		mcp.WithString("role", mcp.Description("Task-specific role chosen by ChatGPT")),
		mcp.WithString("provider", mcp.Description("Provider label chosen by ChatGPT; execution is always routed through the configured 9Router endpoint")),
		mcp.WithString("model", mcp.Description("9Router model identifier chosen by ChatGPT; defaults to the configured auto model")),
		mcp.WithNumber("timeout_seconds", mcp.Description("Timeout chosen by ChatGPT, between 30 and 3600 seconds")),
		mcp.WithBoolean("requires_images", mcp.Description("Whether this task consumes an image")),
		mcp.WithBoolean("model_supports_images", mcp.Description("Explicit capability confirmation. Never infer this from a model name.")),
		mcp.WithString("image_data_url", mcp.Description("Optional in-memory data:image URL. Tautline does not persist this value.")),
		mcp.WithOutputSchema[agentRunView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	maybeSetWidgetMeta("delegate_task", &delegateTool, toolCardWidgetURI, "Delegating task", "Task delegated")
	s.AddTool(delegateTool, handleDelegateTask)

	getTool := mcp.NewTool("get_agent_run",
		mcp.WithTitleAnnotation("Get agent run"),
		mcp.WithDescription("Read the current activity, model, task, output preview, and status of one delegated sub-agent run."),
		mcp.WithString("run_id", mcp.Required(), mcp.Description("Run ID returned by delegate_task")),
		mcp.WithBoolean("wait", mcp.Description("Wait briefly for a running task to change state")),
		mcp.WithNumber("wait_seconds", mcp.Description("Wait duration from 1 to 30 seconds; only used when wait is true")),
		mcp.WithOutputSchema[agentRunView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	maybeSetWidgetMeta("get_agent_run", &getTool, toolCardWidgetURI, "Checking agent", "Agent status ready")
	s.AddTool(getTool, handleGetAgentRun)

	cancelTool := mcp.NewTool("cancel_agent_run",
		mcp.WithTitleAnnotation("Cancel agent run"),
		mcp.WithDescription("Cancel one queued or running sub-agent task by exact run ID."),
		mcp.WithString("run_id", mcp.Required(), mcp.Description("Run ID returned by delegate_task")),
		mcp.WithOutputSchema[agentRunView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	maybeSetWidgetMeta("cancel_agent_run", &cancelTool, toolCardWidgetURI, "Cancelling agent", "Agent cancelled")
	s.AddTool(cancelTool, handleCancelAgentRun)

	browserTool := mcp.NewTool("lightpanda_fetch",
		mcp.WithTitleAnnotation("Fetch with Lightpanda"),
		mcp.WithDescription("Render one public HTTP or HTTPS page using the configured Lightpanda binary. No Chrome fallback is used. Returned HTML stays in memory and is bounded."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Absolute HTTP or HTTPS URL")),
		mcp.WithNumber("timeout_seconds", mcp.Description("Timeout from 5 to 120 seconds")),
		mcp.WithOutputSchema[browserToolView](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	maybeSetWidgetMeta("lightpanda_fetch", &browserTool, toolCardWidgetURI, "Opening page", "Page rendered")
	s.AddTool(browserTool, handleLightpandaFetch)
}

func handleListSubagents(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cfg := runtime.config.snapshot()
	slots := sortedAgentSlots(runtime.agents.slotsSnapshot())
	runs := runtime.agents.runsSnapshot()
	busy := 0
	for _, slot := range slots {
		if slot.Busy {
			busy++
		}
	}
	summary := fmt.Sprintf("%d slots, %d busy", len(slots), busy)
	if !cfg.AgentEnabled {
		summary = "Sub-agent delegation is disabled"
	}
	view := agentCapacityView{
		Kind:          "subagents",
		Title:         "Tautline sub-agents",
		Summary:       summary,
		Enabled:       cfg.AgentEnabled,
		DefaultModel:  cfg.Router.DefaultModel,
		AllowedModels: append([]string(nil), cfg.Router.AllowedModels...),
		Slots:         slots,
		Runs:          runs,
		Router:        runtime.agents.routerStatusSnapshot(),
	}
	return newToolResult("list_subagents", view, view, view.Summary), nil
}

func handleDelegateTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	run, err := runtime.agents.delegate(AgentDelegateRequest{
		Task:                argStr(req, "task"),
		WorkspaceID:         argStr(req, "workspace_id"),
		AgentID:             argStr(req, "agent_id"),
		Name:                argStr(req, "name"),
		Role:                argStr(req, "role"),
		Provider:            argStr(req, "provider"),
		Model:               argStr(req, "model"),
		TimeoutSeconds:      argInt(req, "timeout_seconds", 0),
		RequiresImages:      argBool(req, "requires_images", false),
		ModelSupportsImages: argBool(req, "model_supports_images", false),
		ImageDataURL:        argStr(req, "image_data_url"),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := agentRunView{Kind: "agent_run", Run: run}
	fallback := fmt.Sprintf("Delegated %s to %s as %s using %s.", run.ID, run.SlotID, displayAgentName(run), run.Model)
	return newToolResult("delegate_task", view, view, fallback), nil
}

func handleGetAgentRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	runID := strings.TrimSpace(argStr(req, "run_id"))
	run, ok := runtime.agents.getRun(runID)
	if !ok {
		return mcp.NewToolResultError("unknown agent run " + runID), nil
	}
	if argBool(req, "wait", false) && (run.Status == "queued" || run.Status == "running") {
		seconds := argInt(req, "wait_seconds", 5)
		if seconds < 1 {
			seconds = 1
		}
		if seconds > 30 {
			seconds = 30
		}
		initial := run.UpdatedAt
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				goto finished
			case <-ticker.C:
				candidate, exists := runtime.agents.getRun(runID)
				if !exists {
					goto finished
				}
				run = candidate
				if run.UpdatedAt.After(initial) || (run.Status != "queued" && run.Status != "running") {
					goto finished
				}
			}
		}
	}
finished:
	view := agentRunView{Kind: "agent_run", Run: run}
	fallback := fmt.Sprintf("Agent %s is %s: %s", run.ID, run.Status, run.Activity)
	return newToolResult("get_agent_run", view, view, fallback), nil
}

func handleCancelAgentRun(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	runID := strings.TrimSpace(argStr(req, "run_id"))
	if err := runtime.agents.cancelRun(runID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	run, _ := runtime.agents.getRun(runID)
	view := agentRunView{Kind: "agent_run", Run: run}
	return newToolResult("cancel_agent_run", view, view, "Cancelled agent run "+runID+"."), nil
}

func handleLightpandaFetch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	seconds := argInt(req, "timeout_seconds", 45)
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 120 {
		seconds = 120
	}
	fetchContext, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	result, err := runtime.lightpanda.fetch(fetchContext, argStr(req, "url"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := browserToolView{
		Kind:               "browser",
		Title:              "Lightpanda page",
		Summary:            fmt.Sprintf("%d bytes in %dms", result.Bytes, result.Duration),
		BrowserFetchResult: result,
	}
	return newToolResult("lightpanda_fetch", view, view, view.Summary), nil
}

func displayAgentName(run AgentRun) string {
	if run.Name != "" {
		return run.Name
	}
	if run.AgentID != "" {
		return run.AgentID
	}
	return "a temporary sub-agent"
}
