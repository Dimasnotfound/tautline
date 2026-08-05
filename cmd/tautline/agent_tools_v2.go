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
	Kind          string            `json:"kind"`
	Title         string            `json:"title"`
	Summary       string            `json:"summary"`
	Enabled       bool              `json:"enabled"`
	Backend       string            `json:"backend"`
	DefaultModel  string            `json:"default_model,omitempty"`
	AllowedModels []string          `json:"allowed_models,omitempty"`
	Slots         []AgentSlot       `json:"slots"`
	Runs          []AgentRun        `json:"runs,omitempty"`
	Router        RouterStatus      `json:"router,omitempty"`
	RelayBridge   relayBridgeStatus `json:"relay_bridge"`
}

type agentRunView struct {
	Kind           string                   `json:"kind"`
	Run            AgentRun                 `json:"run"`
	WorkerPrompt   string                   `json:"worker_prompt,omitempty"`
	BridgeDelivery *relayBridgeDeliveryView `json:"bridge_delivery,omitempty"`
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
	s.AddTool(listTool, handleListSubagents)

	delegateTool := mcp.NewTool("delegate_task",
		mcp.WithTitleAnnotation("Delegate task"),
		mcp.WithDescription("Create one asynchronous sub-agent run. The default chatgpt-relay backend queues worker_prompt for the optional Laju Relay Bridge, which opens a fresh ordinary ChatGPT worker tab automatically; worker_prompt remains available as a manual fallback. No Codex, API model call, private ChatGPT backend, or legacy 9Router request is used. Use get_agent_run to inspect progress."),
		mcp.WithString("task", mcp.Required(), mcp.Description("Complete delegated instruction")),
		mcp.WithString("workspace_id", mcp.Description("Optional workspace_id previously returned by open_workspace; enables read-only workspace tools for the sub-agent")),
		mcp.WithString("agent_id", mcp.Description("Temporary logical agent ID chosen by ChatGPT")),
		mcp.WithString("name", mcp.Description("Temporary human-readable name chosen by ChatGPT")),
		mcp.WithString("role", mcp.Description("Task-specific role chosen by ChatGPT")),
		mcp.WithString("provider", mcp.Description("Optional provider label used only by the legacy 9router backend; ignored by chatgpt-relay")),
		mcp.WithString("model", mcp.Description("Optional model identifier used only by the legacy 9router backend; ignored by chatgpt-relay")),
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
	s.AddTool(cancelTool, handleCancelAgentRun)

	claimTool := mcp.NewTool("claim_agent_task",
		mcp.WithTitleAnnotation("Claim ChatGPT relay task"),
		mcp.WithDescription("Claim one pending chatgpt-relay task from a new ordinary ChatGPT conversation using the one-time join code in delegate_task.worker_prompt. Follow the returned instructions and keep worker_token private."),
		mcp.WithString("join_code", mcp.Required(), mcp.Description("One-time join code copied from the main ChatGPT conversation")),
		mcp.WithOutputSchema[agentWorkerAssignment](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(claimTool, handleClaimAgentTask)

	updateTool := mcp.NewTool("update_agent_run",
		mcp.WithTitleAnnotation("Update ChatGPT relay progress"),
		mcp.WithDescription("Update meaningful progress for the chatgpt-relay run claimed by this worker conversation. Never expose worker_token in user-visible text or files."),
		mcp.WithString("run_id", mcp.Required(), mcp.Description("Run ID returned by claim_agent_task")),
		mcp.WithString("worker_token", mcp.Required(), mcp.Description("Private worker token returned by claim_agent_task")),
		mcp.WithString("activity", mcp.Required(), mcp.Description("Concise current activity")),
		mcp.WithOutputSchema[agentRunView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(updateTool, handleUpdateAgentRun)

	completeTool := mcp.NewTool("complete_agent_task",
		mcp.WithTitleAnnotation("Complete ChatGPT relay task"),
		mcp.WithDescription("Complete the chatgpt-relay task claimed by this worker conversation. Call exactly once before the worker's final response. Provide error only when the delegated task failed."),
		mcp.WithString("run_id", mcp.Required(), mcp.Description("Run ID returned by claim_agent_task")),
		mcp.WithString("worker_token", mcp.Required(), mcp.Description("Private worker token returned by claim_agent_task")),
		mcp.WithString("output", mcp.Required(), mcp.Description("Bounded final result for the main ChatGPT conversation")),
		mcp.WithString("error", mcp.Description("Optional failure message; omission marks the run completed")),
		mcp.WithOutputSchema[agentRunView](),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.AddTool(completeTool, handleCompleteAgentTask)

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
		Kind:        "subagents",
		Title:       "Tautline sub-agents",
		Summary:     summary,
		Enabled:     cfg.AgentEnabled,
		Backend:     cfg.AgentBackend,
		Slots:       slots,
		Runs:        runs,
		RelayBridge: runtime.relayBridge.status(),
	}
	if cfg.AgentBackend == agentBackend9Router {
		view.DefaultModel = cfg.Router.DefaultModel
		view.AllowedModels = append([]string(nil), cfg.Router.AllowedModels...)
		view.Router = runtime.agents.routerStatusSnapshot()
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
	if prompt, ok := runtime.agents.chatGPTRelayWorkerPrompt(run.ID); ok {
		view.WorkerPrompt = prompt
		delivery := runtime.relayBridge.enqueue(run.ID, prompt, run.TimeoutSeconds)
		view.BridgeDelivery = &delivery
	}
	fallback := fmt.Sprintf("Delegated %s to %s as %s using %s.", run.ID, run.SlotID, displayAgentName(run), run.Provider)
	if view.BridgeDelivery != nil && view.BridgeDelivery.Connected {
		fallback += " Laju Relay Bridge queued worker_prompt for automatic delivery to a fresh ChatGPT tab."
	} else if view.WorkerPrompt != "" {
		fallback += " Laju Relay Bridge is disconnected; open a new ordinary ChatGPT conversation and paste worker_prompt as the fallback."
	}
	activityView := view
	activityView.WorkerPrompt = ""
	return newToolResult("delegate_task", view, activityView, fallback), nil
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
	if argBool(req, "wait", false) && isAgentRunActiveStatus(run.Status) {
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
				if run.UpdatedAt.After(initial) || !isAgentRunActiveStatus(run.Status) {
					goto finished
				}
			}
		}
	}
finished:
	view := agentRunView{Kind: "agent_run", Run: run}
	if run.Provider == "ChatGPT" {
		delivery := runtime.relayBridge.deliveryView(run.ID)
		view.BridgeDelivery = &delivery
	}
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
	runtime.relayBridge.markRun(runID, "cancelled")
	run, _ := runtime.agents.getRun(runID)
	view := agentRunView{Kind: "agent_run", Run: run}
	return newToolResult("cancel_agent_run", view, view, "Cancelled agent run "+runID+"."), nil
}

func relayAgentToolError(toolName string, err error) *mcp.CallToolResult {
	message := err.Error()
	result := mcp.NewToolResultError(message)
	result.Meta = mcp.NewMetaFromMap(map[string]any{
		activityPendingMeta: map[string]any{
			"payload": map[string]any{
				"kind":    "agent_run",
				"title":   "ChatGPT relay error",
				"summary": message,
				"status":  "error",
				"tool":    toolName,
			},
			"fallback": message,
		},
	})
	return result
}

func handleClaimAgentTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return relayAgentToolError("claim_agent_task", err), nil
	}
	assignment, err := runtime.agents.claimChatGPTRelayTask(argStr(req, "join_code"))
	if err != nil {
		return relayAgentToolError("claim_agent_task", err), nil
	}
	runtime.relayBridge.markRun(assignment.RunID, "claimed")
	fallback := "Claimed ChatGPT relay task " + assignment.RunID + ". Keep worker_token private and complete the task before your final response."
	activityAssignment := assignment
	activityAssignment.WorkerToken = ""
	return newToolResult("claim_agent_task", assignment, activityAssignment, fallback), nil
}

func handleUpdateAgentRun(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return relayAgentToolError("update_agent_run", err), nil
	}
	run, err := runtime.agents.updateChatGPTRelayTask(argStr(req, "run_id"), argStr(req, "worker_token"), argStr(req, "activity"))
	if err != nil {
		return relayAgentToolError("update_agent_run", err), nil
	}
	view := agentRunView{Kind: "agent_run", Run: run}
	return newToolResult("update_agent_run", view, view, "Updated agent run "+run.ID+": "+run.Activity), nil
}

func handleCompleteAgentTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runtime, err := currentApplicationRuntime()
	if err != nil {
		return relayAgentToolError("complete_agent_task", err), nil
	}
	run, err := runtime.agents.completeChatGPTRelayTask(argStr(req, "run_id"), argStr(req, "worker_token"), argStr(req, "output"), argStr(req, "error"))
	if err != nil {
		return relayAgentToolError("complete_agent_task", err), nil
	}
	runtime.relayBridge.markRun(run.ID, run.Status)
	view := agentRunView{Kind: "agent_run", Run: run}
	return newToolResult("complete_agent_task", view, view, "Completed ChatGPT relay task "+run.ID+" with status "+run.Status+"."), nil
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
