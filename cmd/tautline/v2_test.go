package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func newTestConfigStore(t *testing.T, routerURL string) *configStore {
	t.Helper()
	runtimeDir := t.TempDir()
	cfg := defaultTautlineConfig()
	cfg.RuntimeDir = runtimeDir
	cfg.Port = "7688"
	if routerURL != "" {
		cfg.AgentBackend = agentBackend9Router
		cfg.Router.BaseURL = strings.TrimRight(routerURL, "/") + "/v1"
	}
	cfg.Router.DefaultModel = "test-model"
	cfg.Router.AllowedModels = []string{"test-model", "backup-model"}
	cfg.Lightpanda.Executable = filepath.Join(runtimeDir, executableName("missing-lightpanda"))
	cfg.Tunnel.Executable = filepath.Join(runtimeDir, executableName("missing-cloudflared"))
	store := &configStore{
		path:  filepath.Join(runtimeDir, "config", "tautline.json"),
		value: cfg,
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestEffectiveTunnelModeUsesNamedConfigWhenAutostartModeIsOff(t *testing.T) {
	cfg := TunnelConfig{Mode: "off", Name: "devspace", AutoStart: true}
	if got := effectiveTunnelMode("", cfg); got != "named" {
		t.Fatalf("effective tunnel mode = %q, want named", got)
	}
	if got := effectiveTunnelMode("quick", cfg); got != "quick" {
		t.Fatalf("explicit tunnel mode = %q, want quick", got)
	}
	if got := effectiveTunnelMode("", TunnelConfig{Mode: "off", AutoStart: true}); got != "quick" {
		t.Fatalf("quick fallback tunnel mode = %q, want quick", got)
	}
	if got := effectiveTunnelMode("", TunnelConfig{Mode: "named", Name: "devspace"}); got != "" {
		t.Fatalf("disabled tunnel mode = %q, want empty", got)
	}
}

func newTestApplicationRuntime(t *testing.T, routerURL string) *applicationRuntime {
	t.Helper()
	app, err := newApplicationRuntime(newTestConfigStore(t, routerURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.shutdown)
	return app
}

func newFakeRouter(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"data": []map[string]any{{
					"id":       "test-model",
					"owned_by": "test-provider",
					"metadata": map[string]any{"supports_images": true},
				}},
			})
		case "/v1/chat/completions":
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-r.Context().Done():
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id":    "chatcmpl-test",
				"model": "test-model",
				"choices": []map[string]any{{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "verified sub-agent output",
					},
				}},
				"usage": map[string]any{"total_tokens": 12},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func waitForAgentTerminal(t *testing.T, manager *agentManager, runID string) AgentRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, exists := manager.getRun(runID)
		if !exists {
			t.Fatalf("agent run %s disappeared", runID)
		}
		if !isAgentRunActiveStatus(run.Status) {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, _ := manager.getRun(runID)
	t.Fatalf("agent run did not finish: %+v", run)
	return AgentRun{}
}

func TestAgentCanContinueBeyondFormerToolRoundLimit(t *testing.T) {
	var chatCalls atomic.Int32
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"data": []map[string]any{{"id": "test-model", "owned_by": "test-provider"}},
			})
		case "/v1/chat/completions":
			call := chatCalls.Add(1)
			message := map[string]any{"role": "assistant"}
			finishReason := "tool_calls"
			if call <= 10 {
				message["tool_calls"] = []map[string]any{{
					"id":   "loop-probe",
					"type": "function",
					"function": map[string]any{
						"name":      "loop_probe",
						"arguments": "{}",
					},
				}}
			} else {
				finishReason = "stop"
				message["content"] = "completed after extended tool loop"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id":    "chatcmpl-loop-test",
				"model": "test-model",
				"choices": []map[string]any{{
					"index":         0,
					"finish_reason": finishReason,
					"message":       message,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer router.Close()

	app := newTestApplicationRuntime(t, router.URL)
	run, err := app.agents.delegate(AgentDelegateRequest{
		Task:           "Continue using tools until the router returns a final answer.",
		Model:          "test-model",
		TimeoutSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForAgentTerminal(t, app.agents, run.ID)
	if finished.Status != "completed" || finished.Output != "completed after extended tool loop" {
		t.Fatalf("agent did not continue beyond eight tool rounds: %+v", finished)
	}
	if calls := chatCalls.Load(); calls != 11 {
		t.Fatalf("unexpected router call count: got %d want 11", calls)
	}
}

func TestDetectImageSupportFrom9RouterCapabilities(t *testing.T) {
	if got := detectImageSupport(map[string]any{"vision": true}); got != "yes" {
		t.Fatalf("vision capability was not detected: %q", got)
	}
	if got := detectImageSupport(map[string]any{"vision": false}); got != "no" {
		t.Fatalf("negative vision capability was not detected: %q", got)
	}
	if got := detectImageSupport(map[string]any{}, map[string]any{"supports_images": "supported"}); got != "yes" {
		t.Fatalf("metadata image capability was not detected: %q", got)
	}
	if got := detectImageSupport(map[string]any{}); got != "unknown" {
		t.Fatalf("missing capability should remain unknown: %q", got)
	}
}

func TestLightpandaDockerRunnerNeverFallsBackToChrome(t *testing.T) {
	runner := lightpandaRunner{
		mode:        "docker",
		executable:  "docker",
		dockerImage: "lightpanda/browser:nightly",
		container:   "tautline-lightpanda-test",
	}
	cfg := defaultTautlineConfig().Lightpanda
	command := lightpandaServeCommand(runner, cfg)
	joined := strings.ToLower(strings.Join(command.Args, " "))
	for _, required := range []string{"lightpanda/browser:nightly", "serve", "--host", "0.0.0.0"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Lightpanda docker command is missing %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"chrome", "chromium", "playwright"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("browser runner contains forbidden fallback %q: %s", forbidden, joined)
		}
	}
}

func TestParseTunnelConnectorCommandDetectsOrigin(t *testing.T) {
	origin, matched := parseTunnelConnectorCommand("devspace", `bin\cloudflared.exe tunnel --url http://127.0.0.1:7676 run devspace`)
	if !matched || origin != "http://127.0.0.1:7676" {
		t.Fatalf("unexpected connector parse: matched=%t origin=%q", matched, origin)
	}
	if _, matched := parseTunnelConnectorCommand("other", `bin\cloudflared.exe tunnel --url http://127.0.0.1:7676 run devspace`); matched {
		t.Fatal("connector for a different tunnel name was accepted")
	}
	origin, matched = parseTunnelConnectorCommand("devspace", `cloudflared tunnel --url="http://127.0.0.1:7688" run "devspace"`)
	if !matched || origin != "http://127.0.0.1:7688" {
		t.Fatalf("quoted connector was not parsed: matched=%t origin=%q", matched, origin)
	}
}

func TestTunnelCommandUsesLoopbackOriginHost(t *testing.T) {
	cfg := defaultTautlineConfig()
	args := tunnelCommandArgs(cfg, "named", "http://127.0.0.1:7676")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--http-host-header localhost") {
		t.Fatalf("tunnel command does not override origin Host: %s", joined)
	}
	if !strings.Contains(joined, "run "+cfg.Tunnel.Name) {
		t.Fatalf("named tunnel command is missing tunnel name: %s", joined)
	}
}

func TestRouterModelAllowlistIsNormalized(t *testing.T) {
	cfg := defaultTautlineConfig()
	cfg.Router.DefaultModel = "missing-model"
	cfg.Router.AllowedModels = []string{"model-a", " model-b ", "model-a", ""}
	if err := validateTautlineConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Router.AllowedModels, ","); got != "model-a,model-b" {
		t.Fatalf("unexpected normalized allowlist: %q", got)
	}
	if cfg.Router.DefaultModel != "model-a" {
		t.Fatalf("default model was not constrained to the allowlist: %q", cfg.Router.DefaultModel)
	}
}

func TestRouterModelEnvAddsAllowedModel(t *testing.T) {
	t.Setenv("TAUTLINE_9ROUTER_MODEL", "env-model")
	t.Setenv("TAUTLINE_9ROUTER_ALLOWED_MODELS", "")
	cfg := defaultTautlineConfig()
	applyTautlineEnvironment(&cfg)
	if err := validateTautlineConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Router.DefaultModel != "env-model" || !modelAllowed("env-model", cfg.Router.AllowedModels) {
		t.Fatalf("model override missing from allowlist: %+v", cfg.Router)
	}
}

func TestSubAgentGlobalToggleAndModelAllowlist(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	if err := app.config.update(func(cfg *TautlineConfig) error {
		cfg.AgentEnabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.agents.delegate(AgentDelegateRequest{Task: "Must be rejected"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("globally disabled delegation was not rejected: %v", err)
	}
	if err := app.config.update(func(cfg *TautlineConfig) error {
		cfg.AgentEnabled = true
		cfg.AgentBackend = agentBackend9Router
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.agents.delegate(AgentDelegateRequest{Task: "Must be rejected", Model: "forbidden-model"}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("model outside the allowlist was not rejected: %v", err)
	}
}

func TestDefaultAgentBackendUsesChatGPTRelay(t *testing.T) {
	cfg := defaultTautlineConfig()
	if cfg.AgentBackend != agentBackendChatGPTRelay {
		t.Fatalf("default agent backend=%q, want %q", cfg.AgentBackend, agentBackendChatGPTRelay)
	}
	cfg.AgentBackend = "invalid"
	if err := validateTautlineConfig(&cfg); err == nil || !strings.Contains(err.Error(), "agent backend") {
		t.Fatalf("invalid agent backend was accepted: %v", err)
	}
}

func TestChatGPTRelayDoesNotProbeLegacyRouter(t *testing.T) {
	var requests atomic.Int32
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer router.Close()

	store := newTestConfigStore(t, "")
	if err := store.update(func(cfg *TautlineConfig) error {
		cfg.AgentBackend = agentBackendChatGPTRelay
		cfg.Router.BaseURL = router.URL + "/v1"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app, err := newApplicationRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	status := app.agents.refreshRouterStatus(context.Background())
	if requests.Load() != 0 || status.Reachable || status.BaseURL != "" {
		t.Fatalf("chatgpt-relay touched the legacy router: requests=%d status=%+v", requests.Load(), status)
	}
}

func TestChatGPTRelayClaimProgressAndComplete(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	run, err := app.agents.delegate(AgentDelegateRequest{
		Task:           "Review the relay implementation",
		WorkspaceID:    "ws_relay",
		AgentID:        "relay-reviewer",
		Name:           "Relay Reviewer",
		Role:           "Review only",
		TimeoutSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "waiting_worker" || run.Provider != "ChatGPT" || run.Model != "current-chat" {
		t.Fatalf("unexpected relay run: %+v", run)
	}
	prompt, ok := app.agents.chatGPTRelayWorkerPrompt(run.ID)
	if !ok || !strings.Contains(prompt, "@Tautline") || !strings.Contains(prompt, "join_") {
		t.Fatalf("relay worker prompt is missing: %q", prompt)
	}
	joinCode := ""
	for _, field := range strings.Fields(prompt) {
		if strings.HasPrefix(strings.ToLower(field), "join_") {
			joinCode = strings.TrimRight(field, ".,")
			break
		}
	}
	if joinCode == "" {
		t.Fatalf("join code was not found in worker prompt: %q", prompt)
	}
	if _, err := app.agents.claimChatGPTRelayTask("join_WRONG"); err == nil {
		t.Fatal("invalid relay join code was accepted")
	}
	assignment, err := app.agents.claimChatGPTRelayTask(joinCode)
	if err != nil {
		t.Fatal(err)
	}
	if assignment.RunID != run.ID || assignment.WorkerToken == "" || assignment.Task != run.Task || assignment.WorkspaceID != "ws_relay" {
		t.Fatalf("unexpected relay assignment: %+v", assignment)
	}
	if _, err := app.agents.claimChatGPTRelayTask(joinCode); err == nil {
		t.Fatal("one-time relay join code was accepted twice")
	}
	if _, err := app.agents.updateChatGPTRelayTask(run.ID, "wrong-token", "reviewing"); err == nil {
		t.Fatal("invalid worker token updated the run")
	}
	updated, err := app.agents.updateChatGPTRelayTask(run.ID, assignment.WorkerToken, "Reviewing the changed files "+assignment.WorkerToken)
	if err != nil || updated.Phase != "working" || strings.Contains(updated.Activity, assignment.WorkerToken) || !strings.Contains(updated.Activity, "[REDACTED]") {
		t.Fatalf("relay progress update failed or leaked its token: run=%+v err=%v", updated, err)
	}
	completed, err := app.agents.completeChatGPTRelayTask(run.ID, assignment.WorkerToken, "Review passed "+assignment.WorkerToken, "")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || strings.Contains(completed.Output, assignment.WorkerToken) || !strings.Contains(completed.Output, "[REDACTED]") || completed.CompletedAt == nil {
		t.Fatalf("relay task did not complete safely: %+v", completed)
	}
	slots := app.agents.slotsSnapshot()
	if len(slots) == 0 || slots[0].Busy || slots[0].ActiveRunID != "" {
		t.Fatalf("relay slot was not released: %+v", slots)
	}
}

func TestChatGPTRelayCancellationInvalidatesJoinCode(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	run, err := app.agents.delegate(AgentDelegateRequest{Task: "Wait for a worker", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	prompt, ok := app.agents.chatGPTRelayWorkerPrompt(run.ID)
	if !ok {
		t.Fatal("relay prompt was not created")
	}
	joinCode := ""
	for _, field := range strings.Fields(prompt) {
		if strings.HasPrefix(strings.ToLower(field), "join_") {
			joinCode = strings.TrimRight(field, ".,")
			break
		}
	}
	if err := app.agents.cancelRun(run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.agents.claimChatGPTRelayTask(joinCode); err == nil {
		t.Fatal("cancelled relay join code remained claimable")
	}
	cancelled, _ := app.agents.getRun(run.ID)
	if cancelled.Status != "cancelled" || cancelled.CompletedAt == nil {
		t.Fatalf("relay cancellation did not become terminal: %+v", cancelled)
	}
	if slots := app.agents.slotsSnapshot(); len(slots) == 0 || slots[0].Busy {
		t.Fatalf("cancelled relay did not release its slot: %+v", slots)
	}
}

func TestDelegateTaskKeepsRelayJoinCodeOutOfActivityMetadata(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })
	result, err := handleDelegateTask(context.Background(), toolRequest("delegate_task", map[string]any{
		"task":            "Inspect relay join code handling",
		"timeout_seconds": 60,
	}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("delegate_task failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[agentRunView](t, result.StructuredContent)
	if !strings.Contains(view.WorkerPrompt, "join_") {
		t.Fatalf("worker prompt has no join code: %+v", view)
	}
	encodedMeta, err := json.Marshal(result.Meta)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(encodedMeta), []byte("join_")) || bytes.Contains(encodedMeta, []byte(`"worker_prompt":`)) {
		t.Fatalf("relay join code leaked into activity metadata: %s", encodedMeta)
	}
}

func TestClaimAgentTaskKeepsWorkerTokenOutOfActivityMetadata(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })
	run, err := app.agents.delegate(AgentDelegateRequest{Task: "Inspect token handling", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	prompt, ok := app.agents.chatGPTRelayWorkerPrompt(run.ID)
	if !ok {
		t.Fatal("relay prompt was not created")
	}
	joinCode := ""
	for _, field := range strings.Fields(prompt) {
		if strings.HasPrefix(strings.ToLower(field), "join_") {
			joinCode = strings.TrimRight(field, ".,")
			break
		}
	}
	result, err := handleClaimAgentTask(context.Background(), toolRequest("claim_agent_task", map[string]any{"join_code": joinCode}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("claim_agent_task failed: result=%+v err=%v", result, err)
	}
	assignment := decodeStructuredResult[agentWorkerAssignment](t, result.StructuredContent)
	if assignment.WorkerToken == "" {
		t.Fatal("worker token was not returned to the worker model")
	}
	encodedMeta, err := json.Marshal(result.Meta)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encodedMeta, []byte(assignment.WorkerToken)) || bytes.Contains(encodedMeta, []byte(`"worker_token"`)) {
		t.Fatalf("worker token leaked into activity metadata: %s", encodedMeta)
	}

	invalidToken := "worker-secret-that-must-not-leak"
	errorResult, err := handleUpdateAgentRun(context.Background(), toolRequest("update_agent_run", map[string]any{
		"run_id":       run.ID,
		"worker_token": invalidToken,
		"activity":     "testing failure metadata",
	}))
	if err != nil || errorResult == nil || !errorResult.IsError {
		t.Fatalf("invalid worker token did not return a tool error: result=%+v err=%v", errorResult, err)
	}
	encodedErrorMeta, err := json.Marshal(errorResult.Meta)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encodedErrorMeta, []byte(invalidToken)) || bytes.Contains(encodedErrorMeta, []byte(`"worker_token"`)) {
		t.Fatalf("worker token leaked from an error path into activity metadata: %s", encodedErrorMeta)
	}
}

func TestChatGPTRelayToolsAreRegistered(t *testing.T) {
	mcpServer := server.NewMCPServer("test", "1.0.0", server.WithToolCapabilities(true))
	registerAgentTools(mcpServer)
	tools := mcpServer.ListTools()
	for _, name := range []string{"list_subagents", "delegate_task", "get_agent_run", "cancel_agent_run", "claim_agent_task", "update_agent_run", "complete_agent_task"} {
		entry, ok := tools[name]
		if !ok {
			t.Fatalf("agent tool %s is not registered", name)
		}
		encoded, err := json.Marshal(entry.Tool)
		if err != nil {
			t.Fatal(err)
		}
		if name == "claim_agent_task" && (!bytes.Contains(encoded, []byte("join_code")) || !bytes.Contains(encoded, []byte("worker_token"))) {
			t.Fatalf("claim_agent_task contract is incomplete: %s", encoded)
		}
		if name == "complete_agent_task" && (!bytes.Contains(encoded, []byte("run_id")) || !bytes.Contains(encoded, []byte("worker_token")) || !bytes.Contains(encoded, []byte("output"))) {
			t.Fatalf("complete_agent_task contract is incomplete: %s", encoded)
		}
	}
}

func TestRouterResponseModelMustRemainAllowed(t *testing.T) {
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "test-model"}}})
		case "/v1/chat/completions":
			writeJSON(w, http.StatusOK, map[string]any{
				"model":   "forbidden-model",
				"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "should not be accepted"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer router.Close()

	app := newTestApplicationRuntime(t, router.URL)
	run, err := app.agents.delegate(AgentDelegateRequest{Task: "Check returned model", Model: "test-model", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForAgentTerminal(t, app.agents, run.ID)
	if finished.Status != "failed" || !strings.Contains(finished.Error, "not allowed") {
		t.Fatalf("router response outside allowlist was accepted: %+v", finished)
	}
}

func TestDashboardIncludesLocalIconAndAgentControls(t *testing.T) {
	data, err := dashboardAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, required := range []string{"/assets/icon.svg", `id="agents-enabled"`, `id="agent-backend"`, `value="chatgpt-relay"`, `id="model-list"`} {
		if !strings.Contains(html, required) {
			t.Fatalf("dashboard HTML is missing %s", required)
		}
	}
	if _, err := dashboardAssets.ReadFile("web/icon.svg"); err != nil {
		t.Fatalf("dashboard icon is not embedded: %v", err)
	}
}

func TestImageTaskRequiresExplicitCapability(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	allowed := true
	if _, err := app.agents.updateSlot("slot-01", nil, &allowed, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err := app.agents.delegate(AgentDelegateRequest{
		Task:                "Inspect this image",
		RequiresImages:      true,
		ModelSupportsImages: false,
		ImageDataURL:        "data:image/png;base64,AAAA",
	})
	if err == nil || !strings.Contains(err.Error(), "model_supports_images") {
		t.Fatalf("unverified image model was not rejected: %v", err)
	}
}

func TestGenericSlotsPersistOnlyCapacityAndToggles(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	if _, err := app.agents.addSlot(); err != nil {
		t.Fatal(err)
	}
	rtk, caveman := true, true
	if _, err := app.agents.updateSlot("slot-01", nil, nil, &rtk, &caveman); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(app.agents.statePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{`"role"`, `"name"`, `"model"`, `"timeout"`, `"provider"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generic slot persistence contains runtime identity field %s: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"rtk": true`) || !strings.Contains(text, `"caveman": true`) {
		t.Fatalf("slot toggles were not persisted: %s", text)
	}
}

func TestDelegatedRunCompletesAndImagePayloadIsNotPersisted(t *testing.T) {
	router := newFakeRouter(t, 0)
	defer router.Close()
	app := newTestApplicationRuntime(t, router.URL)
	allowImages := true
	if _, err := app.agents.updateSlot("slot-01", nil, &allowImages, nil, nil); err != nil {
		t.Fatal(err)
	}
	const marker = "TAUTLINE_IMAGE_MARKER_4391"
	run, err := app.agents.delegate(AgentDelegateRequest{
		Task:                "Describe the supplied test image",
		AgentID:             "vision-helper",
		Name:                "Visual Inspector",
		Role:                "Inspect only the delegated image",
		Provider:            "9Router",
		Model:               "test-model",
		TimeoutSeconds:      60,
		RequiresImages:      true,
		ModelSupportsImages: true,
		ImageDataURL:        "data:image/png;base64," + marker,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForAgentTerminal(t, app.agents, run.ID)
	if finished.Status != "completed" || finished.Output != "verified sub-agent output" {
		t.Fatalf("unexpected run result: %+v", finished)
	}
	walkErr := filepath.WalkDir(app.config.snapshot().RuntimeDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(marker)) {
			t.Fatalf("image payload was persisted in %s", path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}

func TestCancelledRunCannotBeOverwrittenByWorker(t *testing.T) {
	router := newFakeRouter(t, 2*time.Second)
	defer router.Close()
	app := newTestApplicationRuntime(t, router.URL)
	run, err := app.agents.delegate(AgentDelegateRequest{Task: "Wait for cancellation", Model: "test-model", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if err := app.agents.cancelRun(run.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	finished, exists := app.agents.getRun(run.ID)
	if !exists || finished.Status != "cancelled" || finished.CompletedAt == nil {
		t.Fatalf("cancelled run was overwritten: %+v", finished)
	}
}

func TestDashboardAdminKeyPersistsAcrossRestarts(t *testing.T) {
	runtimeDir := t.TempDir()
	first, err := loadOrCreateDashboardKey(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateDashboardKey(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("dashboard admin key did not persist: first=%q second=%q", first, second)
	}
	info, err := os.Stat(dashboardKeyPath(runtimeDir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 32 {
		t.Fatalf("dashboard admin key file is unexpectedly small: %d", info.Size())
	}
}

func TestDashboardRequiresAdminSessionAndCSRF(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	previousOwnerToken := ownerToken
	previousRoots := append([]string(nil), allowedRoots...)
	ownerToken = "dashboard-owner-token"
	allowedRoots = []string{t.TempDir()}
	t.Cleanup(func() {
		ownerToken = previousOwnerToken
		allowedRoots = previousRoots
	})

	mux := http.NewServeMux()
	registerDashboardRoutes(mux, app)

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauthorized.Code != http.StatusNotFound {
		t.Fatalf("dashboard without admin key returned %d", unauthorized.Code)
	}

	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/?admin="+app.adminKey, nil))
	if login.Code != http.StatusSeeOther {
		t.Fatalf("admin bootstrap returned %d", login.Code)
	}
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("admin bootstrap did not set a cookie")
	}

	stateRequest := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	stateRequest.AddCookie(cookies[0])
	stateResponse := httptest.NewRecorder()
	mux.ServeHTTP(stateResponse, stateRequest)
	if stateResponse.Code != http.StatusOK {
		t.Fatalf("authorized state returned %d: %s", stateResponse.Code, stateResponse.Body.String())
	}
	if strings.Contains(stateResponse.Body.String(), ownerToken) {
		t.Fatal("state endpoint exposed the full owner token")
	}

	blockedReveal := httptest.NewRequest(http.MethodPost, "/api/token/reveal", http.NoBody)
	blockedReveal.AddCookie(cookies[0])
	blockedResponse := httptest.NewRecorder()
	mux.ServeHTTP(blockedResponse, blockedReveal)
	if blockedResponse.Code != http.StatusForbidden {
		t.Fatalf("token reveal without CSRF returned %d", blockedResponse.Code)
	}

	reveal := httptest.NewRequest(http.MethodPost, "/api/token/reveal", http.NoBody)
	reveal.AddCookie(cookies[0])
	reveal.Header.Set("X-Tautline-CSRF", app.csrfToken)
	revealResponse := httptest.NewRecorder()
	mux.ServeHTTP(revealResponse, reveal)
	if revealResponse.Code != http.StatusOK || !strings.Contains(revealResponse.Body.String(), ownerToken) {
		t.Fatalf("authorized token reveal failed: %d %s", revealResponse.Code, revealResponse.Body.String())
	}
}

func TestWidgetResourceCanBeListedAndFetchedThroughMCPHTTP(t *testing.T) {
	previousMode := activeWidgetMode
	activeWidgetMode = widgetModeOn
	t.Cleanup(func() { activeWidgetMode = previousMode })

	mcpServer := server.NewMCPServer(
		"Tautline test",
		appVersion,
		server.WithResourceCapabilities(true, true),
	)
	registerWidgetResource(mcpServer)
	handler := server.NewStreamableHTTPServer(mcpServer, server.WithStateful(true), server.WithDisableStreaming(true))
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	initialize := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "tautline-template-test",
				"version": "1.0.0",
			},
		},
	}
	initializeResponse := postMCPJSON(t, testServer.URL, "", initialize)
	sessionID := initializeResponse.Header.Get("Mcp-Session-Id")
	_ = initializeResponse.Body.Close()
	if sessionID == "" {
		t.Fatal("initialize did not return an MCP session ID")
	}

	listResponse := postMCPJSON(t, testServer.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "resources/list", "params": map[string]any{},
	})
	listBody, _ := io.ReadAll(listResponse.Body)
	_ = listResponse.Body.Close()
	if listResponse.StatusCode != http.StatusOK || !bytes.Contains(listBody, []byte(activityWidgetURI)) {
		t.Fatalf("resources/list failed: %d %s", listResponse.StatusCode, listBody)
	}
	if bytes.Count(listBody, []byte("ui://tautline/")) != 1 {
		t.Fatalf("resources/list returned more than one Tautline widget: %s", listBody)
	}

	readResponse := postMCPJSON(t, testServer.URL, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "resources/read", "params": map[string]any{"uri": activityWidgetURI},
	})
	readBody, _ := io.ReadAll(readResponse.Body)
	_ = readResponse.Body.Close()
	if readResponse.StatusCode != http.StatusOK {
		t.Fatalf("resources/read returned %d: %s", readResponse.StatusCode, readBody)
	}
	for _, marker := range []string{activityWidgetURI, widgetMIMEType, "Tautline activity", "activity_snapshot", "monitor_id", "Latest", "tools/call"} {
		if !bytes.Contains(readBody, []byte(marker)) {
			t.Fatalf("fetched template is missing %q: %s", marker, readBody)
		}
	}
}

func postMCPJSON(t *testing.T, endpoint, sessionID string, payload any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
