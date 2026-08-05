package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelayBridgeDispatchLifecycle(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	clientID := "laju-test-client"
	status := app.relayBridge.heartbeat(clientID, "Laju Browser", "2.10.0")
	if !status.Connected || status.Clients != 1 {
		t.Fatalf("relay bridge did not record the client: %+v", status)
	}

	run, err := app.agents.delegate(AgentDelegateRequest{Task: "Verify automatic relay delivery", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	prompt, ok := app.agents.chatGPTRelayWorkerPrompt(run.ID)
	if !ok {
		t.Fatal("relay worker prompt was not created")
	}
	delivery := app.relayBridge.enqueue(run.ID, prompt, run.TimeoutSeconds)
	if !delivery.Connected || delivery.Status != "queued" || delivery.DispatchID == "" {
		t.Fatalf("relay prompt was not queued: %+v", delivery)
	}

	payload, ok := app.relayBridge.next(context.Background(), clientID, "Laju Browser", "2.10.0", 0)
	if !ok || payload.RunID != run.ID || payload.Prompt != prompt || payload.DispatchID != delivery.DispatchID {
		t.Fatalf("unexpected relay dispatch: %+v ok=%v", payload, ok)
	}
	if err := app.relayBridge.acknowledge(clientID, payload.DispatchID, "sent", ""); err != nil {
		t.Fatal(err)
	}
	delivery = app.relayBridge.deliveryView(run.ID)
	if delivery.Status != "sent" || delivery.Error != "" {
		t.Fatalf("relay dispatch was not acknowledged: %+v", delivery)
	}
	app.relayBridge.markRun(run.ID, "claimed")
	if err := app.relayBridge.acknowledge(clientID, payload.DispatchID, "sent", ""); err != nil {
		t.Fatal(err)
	}
	if delivery = app.relayBridge.deliveryView(run.ID); delivery.Status != "claimed" {
		t.Fatalf("late acknowledgement downgraded claimed relay state: %+v", delivery)
	}
	app.relayBridge.mu.Lock()
	storedPrompt := app.relayBridge.dispatches[payload.DispatchID].Prompt
	app.relayBridge.mu.Unlock()
	if storedPrompt != "" {
		t.Fatal("relay prompt remained in memory after delivery")
	}
}

func TestRelayBridgeHTTPIsLoopbackOnlyAndTokenProtected(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	mux := http.NewServeMux()
	registerRelayBridgeRoutes(mux, app.relayBridge)

	request := httptest.NewRequest(http.MethodGet, relayBridgeAPIBase+"/status", nil)
	request.Host = "127.0.0.1:7688"
	request.RemoteAddr = "127.0.0.1:50000"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated relay bridge status=%d, want %d", response.Code, http.StatusUnauthorized)
	}

	request = httptest.NewRequest(http.MethodGet, relayBridgeAPIBase+"/status", nil)
	request.Host = "127.0.0.1:7688"
	request.RemoteAddr = "127.0.0.1:50000"
	request.Header.Set("Authorization", "Bearer "+app.relayBridge.token)
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("forwarded relay bridge request=%d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodOptions, relayBridgeAPIBase+"/next", nil)
	request.Host = "127.0.0.1:7688"
	request.RemoteAddr = "127.0.0.1:50000"
	request.Header.Set("Origin", "chrome-extension://"+relayBridgeExtensionID)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("extension preflight status=%d origin=%q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}

	request = httptest.NewRequest(http.MethodGet, relayBridgeAPIBase+"/status", nil)
	request.Host = "127.0.0.1:7688"
	request.RemoteAddr = "127.0.0.1:50000"
	request.Header.Set("Origin", "https://chatgpt.com")
	request.Header.Set("Authorization", "Bearer "+app.relayBridge.token)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("website-origin relay bridge request=%d, want %d", response.Code, http.StatusForbidden)
	}

	body := bytes.NewBufferString(`{"client_id":"laju-http-test","browser":"Laju Browser","version":"2.10.0"}`)
	request = httptest.NewRequest(http.MethodPost, relayBridgeAPIBase+"/heartbeat", body)
	request.Host = "localhost:7688"
	request.RemoteAddr = "127.0.0.1:50000"
	request.Header.Set("Authorization", "Bearer "+app.relayBridge.token)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized heartbeat=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), app.relayBridge.token) {
		t.Fatal("relay bridge token leaked into its HTTP response")
	}
	var status relayBridgeStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || !status.Connected {
		t.Fatalf("invalid relay bridge heartbeat response: status=%+v err=%v", status, err)
	}
}

func TestDelegateTaskExposesAutomaticDeliveryWithoutLeakingPromptToActivity(t *testing.T) {
	app := newTestApplicationRuntime(t, "")
	app.relayBridge.heartbeat("laju-tool-test", "Laju Browser", "2.10.0")
	setApplicationRuntime(app)
	t.Cleanup(func() { setApplicationRuntime(nil) })

	result, err := handleDelegateTask(context.Background(), toolRequest("delegate_task", map[string]any{
		"task":            "Verify Laju bridge tool output",
		"timeout_seconds": 60,
	}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("delegate_task failed: result=%+v err=%v", result, err)
	}
	view := decodeStructuredResult[agentRunView](t, result.StructuredContent)
	if view.BridgeDelivery == nil || !view.BridgeDelivery.Connected || view.BridgeDelivery.Status != "queued" {
		t.Fatalf("automatic relay delivery was not exposed: %+v", view.BridgeDelivery)
	}
	encodedMeta, err := json.Marshal(result.Meta)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(encodedMeta), []byte("join_")) || bytes.Contains(encodedMeta, []byte(view.WorkerPrompt)) {
		t.Fatalf("worker prompt leaked into activity metadata: %s", encodedMeta)
	}
}
