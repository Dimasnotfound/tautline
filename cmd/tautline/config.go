package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultTautlinePort       = "7688"
	defaultLightpandaPort     = 9223
	defaultRouterBaseURL      = "http://127.0.0.1:20128/v1"
	defaultAgentCapacity      = 2
	maximumAgentCapacity      = 16
	defaultAgentTimeoutSecond = 900
)

type RouterConfig struct {
	BaseURL       string   `json:"base_url"`
	APIKey        string   `json:"api_key,omitempty"`
	DefaultModel  string   `json:"default_model"`
	AllowedModels []string `json:"allowed_models"`
}

type LightpandaConfig struct {
	Executable  string `json:"executable"`
	DockerImage string `json:"docker_image"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	AutoStart   bool   `json:"auto_start"`
	ObeyRobots  bool   `json:"obey_robots"`
}

type TunnelConfig struct {
	Mode         string `json:"mode"`
	Executable   string `json:"executable"`
	Name         string `json:"name,omitempty"`
	CustomDomain string `json:"custom_domain,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	AutoStart    bool   `json:"auto_start"`
}

type TautlineConfig struct {
	Port              string            `json:"port"`
	RuntimeDir        string            `json:"runtime_dir"`
	OpenDashboard     bool              `json:"open_dashboard"`
	PublicBaseURL     string            `json:"public_base_url,omitempty"`
	WidgetDomain      string            `json:"widget_domain,omitempty"`
	Router            RouterConfig      `json:"router"`
	Lightpanda        LightpandaConfig  `json:"lightpanda"`
	Tunnel            TunnelConfig      `json:"tunnel"`
	AgentEnabled      bool              `json:"agent_enabled"`
	AgentCapacity     int               `json:"agent_capacity"`
	DefaultImageGate  bool              `json:"default_image_gate"`
	DefaultRTK        bool              `json:"default_rtk"`
	DefaultCaveman    bool              `json:"default_caveman"`
	AgentTimeout      int               `json:"agent_timeout_seconds"`
	AdditionalHeaders map[string]string `json:"additional_router_headers,omitempty"`
}

type configStore struct {
	mu    sync.RWMutex
	path  string
	value TautlineConfig
}

func defaultTautlineConfig() TautlineConfig {
	cloudflared := filepath.Join("bin", executableName("cloudflared"))
	return TautlineConfig{
		Port:          defaultTautlinePort,
		RuntimeDir:    filepath.Join("runtime", "v2"),
		OpenDashboard: true,
		Router: RouterConfig{
			BaseURL:       defaultRouterBaseURL,
			DefaultModel:  "auto",
			AllowedModels: []string{"auto"},
		},
		Lightpanda: LightpandaConfig{
			Executable:  "auto",
			DockerImage: "lightpanda/browser:nightly",
			Host:        "127.0.0.1",
			Port:        defaultLightpandaPort,
			AutoStart:   false,
			ObeyRobots:  true,
		},
		Tunnel: TunnelConfig{
			Mode:       "off",
			Executable: cloudflared,
			Protocol:   "http2",
			AutoStart:  false,
		},
		AgentEnabled:     true,
		AgentCapacity:    defaultAgentCapacity,
		DefaultImageGate: false,
		DefaultRTK:       false,
		DefaultCaveman:   false,
		AgentTimeout:     defaultAgentTimeoutSecond,
	}
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func loadTautlineConfig() (*configStore, error) {
	cfg := defaultTautlineConfig()
	path := strings.TrimSpace(os.Getenv("TAUTLINE_CONFIG"))
	if path == "" {
		path = filepath.Join(cfg.RuntimeDir, "config", "tautline.json")
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	applyTautlineEnvironment(&cfg)
	if err := validateTautlineConfig(&cfg); err != nil {
		return nil, err
	}
	store := &configStore{path: path, value: cfg}
	if err := store.save(); err != nil {
		return nil, err
	}
	return store, nil
}

func applyTautlineEnvironment(cfg *TautlineConfig) {
	setStringEnv(&cfg.Port, "TAUTLINE_PORT")
	setStringEnv(&cfg.RuntimeDir, "TAUTLINE_RUNTIME_DIR")
	setStringEnv(&cfg.PublicBaseURL, "TAUTLINE_PUBLIC_BASE_URL")
	setStringEnv(&cfg.WidgetDomain, "TAUTLINE_WIDGET_DOMAIN")
	setStringEnv(&cfg.Router.BaseURL, "TAUTLINE_9ROUTER_BASE_URL")
	setStringEnv(&cfg.Router.APIKey, "TAUTLINE_9ROUTER_API_KEY")
	if model := strings.TrimSpace(os.Getenv("TAUTLINE_9ROUTER_MODEL")); model != "" {
		cfg.Router.DefaultModel = model
		if strings.TrimSpace(os.Getenv("TAUTLINE_9ROUTER_ALLOWED_MODELS")) == "" && !modelAllowed(model, cfg.Router.AllowedModels) {
			cfg.Router.AllowedModels = append(cfg.Router.AllowedModels, model)
		}
	}
	setStringSliceEnv(&cfg.Router.AllowedModels, "TAUTLINE_9ROUTER_ALLOWED_MODELS")
	setStringEnv(&cfg.Lightpanda.Executable, "TAUTLINE_LIGHTPANDA_PATH")
	setStringEnv(&cfg.Lightpanda.DockerImage, "TAUTLINE_LIGHTPANDA_DOCKER_IMAGE")
	setStringEnv(&cfg.Tunnel.Executable, "TAUTLINE_CLOUDFLARED_PATH")
	setStringEnv(&cfg.Tunnel.Name, "TAUTLINE_TUNNEL_NAME")
	setStringEnv(&cfg.Tunnel.CustomDomain, "TAUTLINE_CUSTOM_DOMAIN")
	setStringEnv(&cfg.Tunnel.Mode, "TAUTLINE_TUNNEL_MODE")
	setStringEnv(&cfg.Tunnel.Protocol, "TAUTLINE_TUNNEL_PROTOCOL")
	cfg.OpenDashboard = envBoolWithFallback("TAUTLINE_OPEN_DASHBOARD", cfg.OpenDashboard)
	cfg.Lightpanda.AutoStart = envBoolWithFallback("TAUTLINE_LIGHTPANDA_AUTOSTART", cfg.Lightpanda.AutoStart)
	cfg.Lightpanda.ObeyRobots = envBoolWithFallback("TAUTLINE_LIGHTPANDA_OBEY_ROBOTS", cfg.Lightpanda.ObeyRobots)
	cfg.Tunnel.AutoStart = envBoolWithFallback("TAUTLINE_TUNNEL_AUTOSTART", cfg.Tunnel.AutoStart)
	cfg.AgentEnabled = envBoolWithFallback("TAUTLINE_AGENT_ENABLED", cfg.AgentEnabled)
	cfg.DefaultImageGate = envBoolWithFallback("TAUTLINE_AGENT_IMAGE_SUPPORT", cfg.DefaultImageGate)
	cfg.DefaultRTK = envBoolWithFallback("TAUTLINE_AGENT_RTK", cfg.DefaultRTK)
	cfg.DefaultCaveman = envBoolWithFallback("TAUTLINE_AGENT_CAVEMAN", cfg.DefaultCaveman)
	setIntEnv(&cfg.AgentCapacity, "TAUTLINE_AGENT_CAPACITY")
	setIntEnv(&cfg.AgentTimeout, "TAUTLINE_AGENT_TIMEOUT_SECONDS")
	setIntEnv(&cfg.Lightpanda.Port, "TAUTLINE_LIGHTPANDA_PORT")
}

func firstEnvironment(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func setStringEnv(target *string, key string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*target = value
	}
}

func setStringSliceEnv(target *[]string, key string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*target = splitModelList(value)
	}
}

func splitModelList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
}

func normalizeModelList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		model := strings.TrimSpace(value)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}

func modelAllowed(model string, allowed []string) bool {
	model = strings.TrimSpace(model)
	for _, candidate := range allowed {
		if model == candidate {
			return true
		}
	}
	return false
}

func setIntEnv(target *int, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	parsed, err := strconv.Atoi(value)
	if err == nil {
		*target = parsed
	}
}

func envBoolWithFallback(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func validateTautlineConfig(cfg *TautlineConfig) error {
	cfg.Port = strings.TrimSpace(cfg.Port)
	if cfg.Port == "" {
		cfg.Port = defaultTautlinePort
	}
	if _, err := strconv.Atoi(cfg.Port); err != nil {
		return fmt.Errorf("invalid Tautline port %q", cfg.Port)
	}
	cfg.RuntimeDir = filepath.Clean(strings.TrimSpace(cfg.RuntimeDir))
	if cfg.RuntimeDir == "." || cfg.RuntimeDir == "" {
		cfg.RuntimeDir = filepath.Join("runtime", "v2")
	}
	cfg.Router.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Router.BaseURL), "/")
	if cfg.Router.BaseURL == "" {
		cfg.Router.BaseURL = defaultRouterBaseURL
	}
	parsed, err := url.Parse(cfg.Router.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid 9Router base URL %q", cfg.Router.BaseURL)
	}
	cfg.Router.DefaultModel = strings.TrimSpace(cfg.Router.DefaultModel)
	cfg.Router.AllowedModels = normalizeModelList(cfg.Router.AllowedModels)
	if len(cfg.Router.AllowedModels) == 0 {
		if cfg.Router.DefaultModel == "" {
			cfg.Router.DefaultModel = "auto"
		}
		cfg.Router.AllowedModels = []string{cfg.Router.DefaultModel}
	}
	if cfg.Router.DefaultModel == "" || !modelAllowed(cfg.Router.DefaultModel, cfg.Router.AllowedModels) {
		cfg.Router.DefaultModel = cfg.Router.AllowedModels[0]
	}
	if cfg.AgentCapacity < 1 {
		cfg.AgentCapacity = 1
	}
	if cfg.AgentCapacity > maximumAgentCapacity {
		cfg.AgentCapacity = maximumAgentCapacity
	}
	if cfg.AgentTimeout < 30 {
		cfg.AgentTimeout = 30
	}
	if cfg.AgentTimeout > 3600 {
		cfg.AgentTimeout = 3600
	}
	if strings.TrimSpace(cfg.Lightpanda.Executable) == "" {
		cfg.Lightpanda.Executable = "auto"
	}
	if strings.TrimSpace(cfg.Lightpanda.DockerImage) == "" {
		cfg.Lightpanda.DockerImage = "lightpanda/browser:nightly"
	}
	if cfg.Lightpanda.Host == "" {
		cfg.Lightpanda.Host = "127.0.0.1"
	}
	if cfg.Lightpanda.Port < 1 || cfg.Lightpanda.Port > 65535 {
		cfg.Lightpanda.Port = defaultLightpandaPort
	}
	cfg.Tunnel.Mode = strings.ToLower(strings.TrimSpace(cfg.Tunnel.Mode))
	switch cfg.Tunnel.Mode {
	case "", "off":
		cfg.Tunnel.Mode = "off"
	case "quick", "named":
	default:
		return fmt.Errorf("invalid tunnel mode %q", cfg.Tunnel.Mode)
	}
	return nil
}

func (s *configStore) snapshot() TautlineConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copyValue := s.value
	copyValue.Router.AllowedModels = append([]string(nil), s.value.Router.AllowedModels...)
	if s.value.AdditionalHeaders != nil {
		copyValue.AdditionalHeaders = make(map[string]string, len(s.value.AdditionalHeaders))
		for key, value := range s.value.AdditionalHeaders {
			copyValue.AdditionalHeaders[key] = value
		}
	}
	return copyValue
}

func (s *configStore) update(mutator func(*TautlineConfig) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.value
	if err := mutator(&candidate); err != nil {
		return err
	}
	if err := validateTautlineConfig(&candidate); err != nil {
		return err
	}
	s.value = candidate
	return s.saveLocked()
}

func (s *configStore) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *configStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

func maskedSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not configured"
	}
	if len(value) <= 8 {
		return strings.Repeat("•", len(value))
	}
	return value[:4] + strings.Repeat("•", 8) + value[len(value)-4:]
}
