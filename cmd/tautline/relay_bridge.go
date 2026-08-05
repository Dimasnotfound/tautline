package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	relayBridgeAPIBase       = "/relay-bridge/v1"
	relayBridgeClientTTL     = 20 * time.Second
	relayBridgeDispatchLease = 45 * time.Second
	relayBridgeMaxBodyBytes  = 16 << 10
	relayBridgeMaxErrorBytes = 512
	relayBridgeExtensionID   = "oipiaofdfblejkognebaddegbnfaplph"
)

type relayBridgeStatus struct {
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Connected bool       `json:"connected"`
	Clients   int        `json:"clients"`
	Queued    int        `json:"queued"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	Version   string     `json:"version"`
}

type relayBridgeDeliveryView struct {
	Mode       string `json:"mode"`
	Status     string `json:"status"`
	Connected  bool   `json:"connected"`
	DispatchID string `json:"dispatch_id,omitempty"`
	Attempts   int    `json:"attempts,omitempty"`
	Error      string `json:"error,omitempty"`
	Summary    string `json:"summary"`
}

type relayBridgeDispatchPayload struct {
	Kind       string    `json:"kind"`
	DispatchID string    `json:"dispatch_id"`
	RunID      string    `json:"run_id"`
	Prompt     string    `json:"prompt"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type relayBridgeClient struct {
	ID       string
	Browser  string
	Version  string
	LastSeen time.Time
}

type relayBridgeDispatch struct {
	ID        string
	RunID     string
	Prompt    string
	Status    string
	ClientID  string
	Error     string
	Attempts  int
	CreatedAt time.Time
	ExpiresAt time.Time
	LeasedAt  time.Time
	UpdatedAt time.Time
}

type relayBridgeManager struct {
	mu          sync.Mutex
	token       string
	clients     map[string]relayBridgeClient
	dispatches  map[string]*relayBridgeDispatch
	runDispatch map[string]string
	order       []string
	wake        chan struct{}
}

func newRelayBridgeManager(runtimeDir string) (*relayBridgeManager, error) {
	token, err := loadOrCreateRelayBridgeToken(runtimeDir)
	if err != nil {
		return nil, err
	}
	return &relayBridgeManager{
		token:       token,
		clients:     map[string]relayBridgeClient{},
		dispatches:  map[string]*relayBridgeDispatch{},
		runDispatch: map[string]string{},
		wake:        make(chan struct{}, 1),
	}, nil
}

func relayBridgeTokenPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "state", "relay-bridge.token")
}

func loadOrCreateRelayBridgeToken(runtimeDir string) (string, error) {
	path := relayBridgeTokenPath(runtimeDir)
	if data, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) >= 64 {
			return token, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	token := randomHex(32)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return token, nil
}

func (b *relayBridgeManager) enqueue(runID, prompt string, timeoutSeconds int) relayBridgeDeliveryView {
	runID = strings.TrimSpace(runID)
	prompt = strings.TrimSpace(prompt)
	if runID == "" || prompt == "" {
		return relayBridgeDeliveryView{Mode: "manual", Status: "unavailable", Summary: "No automatic relay prompt is available."}
	}
	if timeoutSeconds < 30 {
		timeoutSeconds = 30
	}
	now := time.Now().UTC()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(now)
	if dispatchID := b.runDispatch[runID]; dispatchID != "" {
		return b.deliveryViewLocked(runID, now)
	}
	dispatch := &relayBridgeDispatch{
		ID:        "dispatch_" + randomHex(12),
		RunID:     runID,
		Prompt:    prompt,
		Status:    "queued",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(timeoutSeconds) * time.Second),
		UpdatedAt: now,
	}
	b.dispatches[dispatch.ID] = dispatch
	b.runDispatch[runID] = dispatch.ID
	b.order = append(b.order, dispatch.ID)
	b.signalLocked()
	return b.deliveryViewLocked(runID, now)
}

func (b *relayBridgeManager) deliveryView(runID string) relayBridgeDeliveryView {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	b.cleanupLocked(now)
	return b.deliveryViewLocked(strings.TrimSpace(runID), now)
}

func (b *relayBridgeManager) deliveryViewLocked(runID string, now time.Time) relayBridgeDeliveryView {
	connected, _, _ := b.connectedLocked(now)
	dispatchID := b.runDispatch[runID]
	dispatch := b.dispatches[dispatchID]
	if dispatch == nil {
		return relayBridgeDeliveryView{
			Mode:      "manual",
			Status:    "not_queued",
			Connected: connected,
			Summary:   "Use worker_prompt as the manual fallback.",
		}
	}
	view := relayBridgeDeliveryView{
		Mode:       "laju-extension",
		Status:     dispatch.Status,
		Connected:  connected,
		DispatchID: dispatch.ID,
		Attempts:   dispatch.Attempts,
		Error:      dispatch.Error,
	}
	switch dispatch.Status {
	case "sent":
		view.Summary = "Laju Relay Bridge submitted the worker prompt to a fresh ChatGPT tab."
	case "dispatched":
		view.Summary = "Laju Relay Bridge is opening a fresh ChatGPT worker tab."
	case "claimed":
		view.Summary = "The ChatGPT worker claimed the task."
	case "failed", "expired", "cancelled":
		view.Summary = "Automatic delivery did not complete; worker_prompt remains the manual fallback."
	default:
		if connected {
			view.Summary = "Worker prompt queued for the connected Laju Relay Bridge."
		} else {
			view.Summary = "Laju Relay Bridge is disconnected; worker_prompt remains available for manual delivery."
		}
	}
	return view
}

func (b *relayBridgeManager) markRun(runID, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	dispatch := b.dispatches[b.runDispatch[strings.TrimSpace(runID)]]
	if dispatch == nil {
		return
	}
	dispatch.Prompt = ""
	dispatch.Status = strings.TrimSpace(status)
	dispatch.UpdatedAt = time.Now().UTC()
}

func (b *relayBridgeManager) status() relayBridgeStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	b.cleanupLocked(now)
	connected, clients, lastSeen := b.connectedLocked(now)
	queued := 0
	for _, dispatch := range b.dispatches {
		if dispatch.Status == "queued" || dispatch.Status == "dispatched" {
			queued++
		}
	}
	return relayBridgeStatus{
		Kind:      "relay_bridge",
		Title:     "Laju Relay Bridge",
		Connected: connected,
		Clients:   clients,
		Queued:    queued,
		LastSeen:  lastSeen,
		Version:   appVersion,
	}
}

func (b *relayBridgeManager) next(ctx context.Context, clientID, browser, version string, wait time.Duration) (relayBridgeDispatchPayload, bool) {
	if wait < 0 {
		wait = 0
	}
	if wait > 25*time.Second {
		wait = 25 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		now := time.Now().UTC()
		b.mu.Lock()
		b.recordClientLocked(clientID, browser, version, now)
		b.cleanupLocked(now)
		if payload, ok := b.nextLocked(clientID, now); ok {
			b.mu.Unlock()
			return payload, true
		}
		wake := b.wake
		b.mu.Unlock()
		if wait == 0 {
			return relayBridgeDispatchPayload{}, false
		}
		select {
		case <-ctx.Done():
			return relayBridgeDispatchPayload{}, false
		case <-timer.C:
			return relayBridgeDispatchPayload{}, false
		case <-wake:
		}
	}
}

func (b *relayBridgeManager) nextLocked(clientID string, now time.Time) (relayBridgeDispatchPayload, bool) {
	for _, dispatchID := range b.order {
		dispatch := b.dispatches[dispatchID]
		if dispatch == nil || dispatch.Status != "queued" || dispatch.Prompt == "" {
			continue
		}
		dispatch.Status = "dispatched"
		dispatch.ClientID = clientID
		dispatch.Attempts++
		dispatch.LeasedAt = now
		dispatch.UpdatedAt = now
		return relayBridgeDispatchPayload{
			Kind:       "relay_dispatch",
			DispatchID: dispatch.ID,
			RunID:      dispatch.RunID,
			Prompt:     dispatch.Prompt,
			ExpiresAt:  dispatch.ExpiresAt,
		}, true
	}
	return relayBridgeDispatchPayload{}, false
}

func (b *relayBridgeManager) acknowledge(clientID, dispatchID, status, errorMessage string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	b.cleanupLocked(now)
	dispatch := b.dispatches[strings.TrimSpace(dispatchID)]
	if dispatch == nil {
		return errors.New("unknown relay dispatch")
	}
	if dispatch.ClientID != "" && dispatch.ClientID != clientID {
		return errors.New("relay dispatch is assigned to another client")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if dispatch.Status == "sent" || dispatch.Status == "claimed" || dispatch.Status == "completed" || dispatch.Status == "cancelled" || dispatch.Status == "timed_out" {
		dispatch.Prompt = ""
		dispatch.UpdatedAt = now
		return nil
	}
	switch status {
	case "sent":
		dispatch.Status = "sent"
		dispatch.Prompt = ""
		dispatch.Error = ""
	case "failed":
		dispatch.Status = "failed"
		dispatch.Prompt = ""
		dispatch.Error = boundString(strings.TrimSpace(errorMessage), relayBridgeMaxErrorBytes)
	default:
		return errors.New("relay acknowledgement status must be sent or failed")
	}
	dispatch.UpdatedAt = now
	return nil
}

func (b *relayBridgeManager) heartbeat(clientID, browser, version string) relayBridgeStatus {
	b.mu.Lock()
	now := time.Now().UTC()
	b.recordClientLocked(clientID, browser, version, now)
	b.mu.Unlock()
	return b.status()
}

func (b *relayBridgeManager) recordClientLocked(clientID, browser, version string, now time.Time) {
	b.clients[clientID] = relayBridgeClient{
		ID:       clientID,
		Browser:  boundString(strings.TrimSpace(browser), 80),
		Version:  boundString(strings.TrimSpace(version), 40),
		LastSeen: now,
	}
}

func (b *relayBridgeManager) connectedLocked(now time.Time) (bool, int, *time.Time) {
	clients := 0
	var latest time.Time
	for _, client := range b.clients {
		if now.Sub(client.LastSeen) > relayBridgeClientTTL {
			continue
		}
		clients++
		if client.LastSeen.After(latest) {
			latest = client.LastSeen
		}
	}
	if clients == 0 {
		return false, 0, nil
	}
	return true, clients, &latest
}

func (b *relayBridgeManager) cleanupLocked(now time.Time) {
	for id, client := range b.clients {
		if now.Sub(client.LastSeen) > 10*relayBridgeClientTTL {
			delete(b.clients, id)
		}
	}
	for id, dispatch := range b.dispatches {
		if dispatch.Status == "dispatched" && now.Sub(dispatch.LeasedAt) > relayBridgeDispatchLease {
			dispatch.Status = "failed"
			dispatch.Prompt = ""
			dispatch.Error = "Laju Relay Bridge did not acknowledge the dispatched prompt"
			dispatch.UpdatedAt = now
		}
		if (dispatch.Status == "queued" || dispatch.Status == "dispatched") && now.After(dispatch.ExpiresAt) {
			dispatch.Status = "expired"
			dispatch.Prompt = ""
			dispatch.UpdatedAt = now
		}
		if dispatch.Prompt == "" && now.Sub(dispatch.UpdatedAt) > time.Hour {
			delete(b.dispatches, id)
			delete(b.runDispatch, dispatch.RunID)
		}
	}
}

func (b *relayBridgeManager) signalLocked() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func registerRelayBridgeRoutes(mux *http.ServeMux, bridge *relayBridgeManager) {
	mux.HandleFunc(relayBridgeAPIBase+"/status", bridge.handleStatus)
	mux.HandleFunc(relayBridgeAPIBase+"/heartbeat", bridge.handleHeartbeat)
	mux.HandleFunc(relayBridgeAPIBase+"/next", bridge.handleNext)
	mux.HandleFunc(relayBridgeAPIBase+"/ack", bridge.handleAck)
}

func (b *relayBridgeManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	if relayBridgeHandlePreflight(w, r) {
		return
	}
	if !b.authorizeHTTP(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeRelayBridgeJSON(w, http.StatusOK, b.status())
}

func (b *relayBridgeManager) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if relayBridgeHandlePreflight(w, r) {
		return
	}
	if !b.authorizeHTTP(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ClientID string `json:"client_id"`
		Browser  string `json:"browser"`
		Version  string `json:"version"`
	}
	if err := decodeRelayBridgeJSON(w, r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateRelayBridgeClientID(input.ClientID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeRelayBridgeJSON(w, http.StatusOK, b.heartbeat(input.ClientID, input.Browser, input.Version))
}

func (b *relayBridgeManager) handleNext(w http.ResponseWriter, r *http.Request) {
	if relayBridgeHandlePreflight(w, r) {
		return
	}
	if !b.authorizeHTTP(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID := strings.TrimSpace(r.URL.Query().Get("client_id"))
	if err := validateRelayBridgeClientID(clientID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	waitSeconds, _ := strconv.Atoi(r.URL.Query().Get("wait_seconds"))
	if waitSeconds < 0 {
		waitSeconds = 0
	}
	if waitSeconds > 25 {
		waitSeconds = 25
	}
	payload, ok := b.next(r.Context(), clientID, r.Header.Get("X-Tautline-Browser"), r.Header.Get("X-Tautline-Bridge-Version"), time.Duration(waitSeconds)*time.Second)
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeRelayBridgeJSON(w, http.StatusOK, payload)
}

func (b *relayBridgeManager) handleAck(w http.ResponseWriter, r *http.Request) {
	if relayBridgeHandlePreflight(w, r) {
		return
	}
	if !b.authorizeHTTP(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ClientID   string `json:"client_id"`
		DispatchID string `json:"dispatch_id"`
		Status     string `json:"status"`
		Error      string `json:"error"`
	}
	if err := decodeRelayBridgeJSON(w, r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateRelayBridgeClientID(input.ClientID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := b.acknowledge(input.ClientID, input.DispatchID, input.Status, input.Error); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeRelayBridgeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func relayBridgeHandlePreflight(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodOptions {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !relayBridgeRequestIsLoopback(r) || !relayBridgeApplyOrigin(w, r) {
		http.Error(w, "relay bridge preflight denied", http.StatusForbidden)
		return true
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Tautline-Browser, X-Tautline-Bridge-Version")
	w.WriteHeader(http.StatusNoContent)
	return true
}

func relayBridgeApplyOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if origin != "chrome-extension://"+relayBridgeExtensionID && origin != "edge-extension://"+relayBridgeExtensionID {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	return true
}

func (b *relayBridgeManager) authorizeHTTP(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !relayBridgeRequestIsLoopback(r) || !relayBridgeApplyOrigin(w, r) {
		http.Error(w, "relay bridge is loopback-only", http.StatusForbidden)
		return false
	}
	const prefix = "Bearer "
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, prefix) || !secureAgentTokenEqual(strings.TrimSpace(strings.TrimPrefix(authorization, prefix)), b.token) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tautline-relay-bridge"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func relayBridgeRequestIsLoopback(r *http.Request) bool {
	for _, header := range []string{"CF-Connecting-IP", "Forwarded", "X-Forwarded-For"} {
		if strings.TrimSpace(r.Header.Get(header)) != "" {
			return false
		}
	}
	host := strings.TrimSpace(r.Host)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	remoteHost, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return false
	}
	remoteIP := net.ParseIP(strings.Trim(remoteHost, "[]"))
	return remoteIP != nil && remoteIP.IsLoopback()
}

func validateRelayBridgeClientID(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 80 {
		return errors.New("client_id must contain 8 to 80 characters")
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return fmt.Errorf("client_id contains unsupported character %q", character)
	}
	return nil
}

func decodeRelayBridgeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, relayBridgeMaxBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func writeRelayBridgeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
