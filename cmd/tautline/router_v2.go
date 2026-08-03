package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxRouterResponseBytes = 16 * 1024 * 1024

type RouterModel struct {
	ID           string `json:"id"`
	OwnedBy      string `json:"owned_by,omitempty"`
	ImageSupport string `json:"image_support"`
}

type RouterStatus struct {
	Reachable bool          `json:"reachable"`
	BaseURL   string        `json:"base_url"`
	Models    []RouterModel `json:"models,omitempty"`
	Error     string        `json:"error,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

type routerModelList struct {
	Data []struct {
		ID           string         `json:"id"`
		OwnedBy      string         `json:"owned_by"`
		Meta         map[string]any `json:"metadata"`
		Capabilities map[string]any `json:"capabilities"`
	} `json:"data"`
}

type routerMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []routerToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type routerToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type routerTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type routerChatRequest struct {
	Model       string          `json:"model"`
	Messages    []routerMessage `json:"messages"`
	Tools       []routerTool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream"`
}

type routerChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		FinishReason string        `json:"finish_reason"`
		Message      routerMessage `json:"message"`
	} `json:"choices"`
	Usage map[string]any `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type routerClient struct {
	store  *configStore
	client *http.Client
}

func newRouterClient(store *configStore) *routerClient {
	return &routerClient{
		store: store,
		client: &http.Client{
			Timeout: 0,
		},
	}
}

func (c *routerClient) status(ctx context.Context) RouterStatus {
	cfg := c.store.snapshot().Router
	status := RouterStatus{BaseURL: cfg.BaseURL, CheckedAt: time.Now().UTC()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/models", nil)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	c.applyHeaders(request, cfg, false, false)
	response, err := c.client.Do(request)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		status.Error = err.Error()
		return status
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status.Error = fmt.Sprintf("9Router returned %s: %s", response.Status, compactError(body))
		return status
	}
	var list routerModelList
	if err := json.Unmarshal(body, &list); err != nil {
		status.Error = "decode 9Router models: " + err.Error()
		return status
	}
	status.Reachable = true
	for _, model := range list.Data {
		status.Models = append(status.Models, RouterModel{
			ID:           model.ID,
			OwnedBy:      model.OwnedBy,
			ImageSupport: detectImageSupport(model.Capabilities, model.Meta),
		})
	}
	return status
}

func detectImageSupport(sources ...map[string]any) string {
	for _, source := range sources {
		for _, key := range []string{"vision", "image", "image_input", "supports_images", "multimodal"} {
			value, exists := source[key]
			if !exists {
				continue
			}
			switch typed := value.(type) {
			case bool:
				if typed {
					return "yes"
				}
				return "no"
			case string:
				normalized := strings.ToLower(strings.TrimSpace(typed))
				switch normalized {
				case "true", "yes", "supported", "enabled":
					return "yes"
				case "false", "no", "unsupported", "disabled":
					return "no"
				}
			}
		}
	}
	return "unknown"
}

func (c *routerClient) complete(ctx context.Context, payload routerChatRequest, rtk, caveman bool) (routerChatResponse, error) {
	cfg := c.store.snapshot().Router
	if strings.TrimSpace(payload.Model) == "" || payload.Model == "auto" {
		payload.Model = cfg.DefaultModel
	}
	if strings.TrimSpace(payload.Model) == "" {
		payload.Model = "auto"
	}
	payload.Stream = false
	encoded, err := json.Marshal(payload)
	if err != nil {
		return routerChatResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return routerChatResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	c.applyHeaders(request, cfg, rtk, caveman)
	response, err := c.client.Do(request)
	if err != nil {
		return routerChatResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRouterResponseBytes))
	if err != nil {
		return routerChatResponse{}, err
	}
	var result routerChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return routerChatResponse{}, fmt.Errorf("decode 9Router response: %w; body=%s", err, compactError(body))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := compactError(body)
		if result.Error != nil && strings.TrimSpace(result.Error.Message) != "" {
			message = result.Error.Message
		}
		return result, fmt.Errorf("9Router returned %s: %s", response.Status, message)
	}
	if result.Error != nil {
		return result, fmt.Errorf("9Router error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return result, fmt.Errorf("9Router returned no choices")
	}
	return result, nil
}

func (c *routerClient) applyHeaders(request *http.Request, cfg RouterConfig, rtk, caveman bool) {
	if token := strings.TrimSpace(cfg.APIKey); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("X-Tautline-RTK", boolText(rtk))
	request.Header.Set("X-Tautline-Caveman", boolText(caveman))
	for key, value := range c.store.snapshot().AdditionalHeaders {
		key = strings.TrimSpace(key)
		if key != "" && !strings.EqualFold(key, "authorization") {
			request.Header.Set(key, value)
		}
	}
}

func boolText(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func compactError(body []byte) string {
	value := strings.Join(strings.Fields(string(body)), " ")
	if len(value) > 500 {
		value = value[:500] + "..."
	}
	if value == "" {
		return "empty response"
	}
	return value
}
