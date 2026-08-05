package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxRelayActivityBytes = 1024

type agentWorkerAssignment struct {
	Kind           string   `json:"kind"`
	RunID          string   `json:"run_id"`
	WorkerToken    string   `json:"worker_token,omitempty"`
	Task           string   `json:"task"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	AgentID        string   `json:"agent_id,omitempty"`
	Name           string   `json:"name,omitempty"`
	Role           string   `json:"role,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Instructions   []string `json:"instructions"`
}

func (m *agentManager) delegateChatGPTRelay(request AgentDelegateRequest, cfg TautlineConfig) (AgentRun, error) {
	if request.RequiresImages || strings.TrimSpace(request.ImageDataURL) != "" {
		return AgentRun{}, errors.New("ChatGPT relay image transfer is not automatic; attach the image manually in the worker chat")
	}

	m.mu.Lock()
	slotIndex := -1
	for index := range m.slots {
		if m.slots[index].Enabled && !m.slots[index].Busy {
			slotIndex = index
			break
		}
	}
	if slotIndex < 0 {
		m.mu.Unlock()
		return AgentRun{}, errors.New("no enabled sub-agent slot is available")
	}

	timeout := request.TimeoutSeconds
	if timeout <= 0 {
		timeout = cfg.AgentTimeout
	}
	if timeout < 30 {
		timeout = 30
	}
	if timeout > 3600 {
		timeout = 3600
	}

	runID := "run_" + randToken()[:16]
	claimCode := "join_" + strings.ToUpper(randomHex(12))
	now := time.Now().UTC()
	slot := &m.slots[slotIndex]
	run := AgentRun{
		ID:              runID,
		SlotID:          slot.ID,
		AgentID:         strings.TrimSpace(request.AgentID),
		Name:            strings.TrimSpace(request.Name),
		Role:            strings.TrimSpace(request.Role),
		Provider:        "ChatGPT",
		Model:           "current-chat",
		Task:            request.Task,
		WorkspaceID:     strings.TrimSpace(request.WorkspaceID),
		RequiresImages:  false,
		ImageCapability: "manual-only",
		RTK:             slot.RTK,
		Caveman:         slot.Caveman,
		Status:          "waiting_worker",
		Phase:           "waiting_worker",
		Activity:        "Waiting for a regular ChatGPT chat to claim the task",
		StartedAt:       now,
		UpdatedAt:       now,
		TimeoutSeconds:  timeout,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	m.runs[runID] = &agentRunState{value: run, cancel: cancel, claimCode: claimCode}
	m.history = append(m.history, runID)
	slot.Busy = true
	slot.ActiveRunID = runID
	m.trimRunsLocked()
	m.mu.Unlock()

	go m.watchChatGPTRelayRun(ctx, runID)
	return run, nil
}

func (m *agentManager) watchChatGPTRelayRun(ctx context.Context, runID string) {
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		m.finishRun(runID, "timed_out", "No ChatGPT worker completed the task before its timeout", "", ctx.Err())
	}
}

func (m *agentManager) chatGPTRelayWorkerPrompt(runID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.runs[strings.TrimSpace(runID)]
	if state == nil || state.claimCode == "" || state.value.Status != "waiting_worker" {
		return "", false
	}
	prompt := fmt.Sprintf("@Tautline Connect this chat as a Tautline worker using code %s. Claim the task, follow the returned instructions and workspace, report meaningful progress with update_agent_run, then call complete_agent_task exactly once before your final response.", state.claimCode)
	return prompt, true
}

func (m *agentManager) claimChatGPTRelayTask(joinCode string) (agentWorkerAssignment, error) {
	joinCode = strings.ToUpper(strings.TrimSpace(joinCode))
	if joinCode == "" {
		return agentWorkerAssignment{}, errors.New("join_code is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, runID := range m.history {
		state := m.runs[runID]
		if state == nil || state.claimCode == "" || !secureAgentTokenEqual(strings.ToUpper(state.claimCode), joinCode) {
			continue
		}
		if state.value.Status != "waiting_worker" {
			return agentWorkerAssignment{}, fmt.Errorf("agent run %s is already %s", runID, state.value.Status)
		}
		now := time.Now().UTC()
		state.workerToken = randToken()
		state.claimCode = ""
		state.value.Status = "running"
		state.value.Phase = "claimed"
		state.value.Activity = "A regular ChatGPT worker claimed the task"
		state.value.ClaimedAt = &now
		state.value.UpdatedAt = now
		return agentWorkerAssignment{
			Kind:           "agent_worker_assignment",
			RunID:          state.value.ID,
			WorkerToken:    state.workerToken,
			Task:           state.value.Task,
			WorkspaceID:    state.value.WorkspaceID,
			AgentID:        state.value.AgentID,
			Name:           state.value.Name,
			Role:           state.value.Role,
			TimeoutSeconds: state.value.TimeoutSeconds,
			Instructions: []string{
				"Perform only the delegated task in this ChatGPT conversation.",
				"Use the returned workspace_id for local work and respect its existing checkout or worktree boundary.",
				"Use update_agent_run for meaningful progress changes, not every small tool call.",
				"Call complete_agent_task exactly once with this run_id and worker_token before your final response.",
				"Never reveal the worker_token to the user or place it in files, commands, logs, or task output.",
			},
		}, nil
	}
	return agentWorkerAssignment{}, errors.New("unknown, expired, or already-used join_code")
}

func (m *agentManager) updateChatGPTRelayTask(runID, workerToken, activity string) (AgentRun, error) {
	activity = strings.TrimSpace(activity)
	if activity == "" {
		return AgentRun{}, errors.New("activity must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.authorizeChatGPTWorkerLocked(runID, workerToken)
	if err != nil {
		return AgentRun{}, err
	}
	if state.value.Status != "running" {
		return AgentRun{}, fmt.Errorf("agent run %s is %s", state.value.ID, state.value.Status)
	}
	state.value.Phase = "working"
	activity = strings.ReplaceAll(activity, state.workerToken, "[REDACTED]")
	state.value.Activity = boundString(activity, maxRelayActivityBytes)
	state.value.UpdatedAt = time.Now().UTC()
	return state.value, nil
}

func (m *agentManager) completeChatGPTRelayTask(runID, workerToken, output, errorMessage string) (AgentRun, error) {
	m.mu.Lock()
	state, err := m.authorizeChatGPTWorkerLocked(runID, workerToken)
	if err != nil {
		m.mu.Unlock()
		return AgentRun{}, err
	}
	if state.value.Status != "running" {
		status := state.value.Status
		m.mu.Unlock()
		return AgentRun{}, fmt.Errorf("agent run %s is already %s", state.value.ID, status)
	}
	secretToken := state.workerToken
	output = strings.ReplaceAll(output, secretToken, "[REDACTED]")
	errorMessage = strings.ReplaceAll(errorMessage, secretToken, "[REDACTED]")
	state.workerToken = ""
	state.value.Status = "finishing"
	state.value.Phase = "finishing"
	state.value.Activity = "ChatGPT worker submitted the final result"
	state.value.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()

	errorMessage = strings.TrimSpace(errorMessage)
	if errorMessage != "" {
		m.finishRun(runID, "failed", "ChatGPT worker reported a failure", output, errors.New(errorMessage))
	} else {
		m.finishRun(runID, "completed", "Task completed by a regular ChatGPT worker", output, nil)
	}
	completed, _ := m.getRun(runID)
	return completed, nil
}

func (m *agentManager) authorizeChatGPTWorkerLocked(runID, workerToken string) (*agentRunState, error) {
	runID = strings.TrimSpace(runID)
	workerToken = strings.TrimSpace(workerToken)
	state := m.runs[runID]
	if state == nil {
		return nil, fmt.Errorf("unknown agent run %q", runID)
	}
	if state.workerToken == "" || workerToken == "" || !secureAgentTokenEqual(state.workerToken, workerToken) {
		return nil, errors.New("invalid worker token")
	}
	return state, nil
}

func secureAgentTokenEqual(left, right string) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func isAgentRunActiveStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "queued", "waiting_worker", "running":
		return true
	default:
		return false
	}
}
