package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxCodexModelInstructionsBytes = 256 * 1024

type hostInstructionStatus struct {
	Source     string `json:"source"`
	Configured bool   `json:"configured"`
	Loaded     bool   `json:"loaded"`
	Fallback   bool   `json:"fallback"`
	Bytes      int    `json:"bytes,omitempty"`
}

// hostInstructions builds the instructions advertised by Tautline's primary MCP
// server to ChatGPT. Relay worker chats receive these host instructions through
// their own MCP connection; legacy 9Router prompts remain separate. Codex
// guidance is optional; Tautline's local tool and safety rules remain
// authoritative and are appended last.
func hostInstructions() (string, hostInstructionStatus, error) {
	workflow := codingWorkflowInstructions()
	codexInstructions, status, err := loadCodexHostGuidance()
	if err != nil {
		status.Fallback = true
		return workflow, status, err
	}
	if strings.TrimSpace(codexInstructions) == "" {
		return workflow, status, nil
	}

	merged := strings.Join([]string{
		"The following imported Codex host guidance is for the primary ChatGPT connection only. It may come from model_instructions_file and the global Codex AGENTS file. It does not grant tools, permissions, sandbox access, or authority beyond the current ChatGPT conversation and Tautline runtime.",
		"<codex_host_guidance>\n" + codexInstructions + "\n</codex_host_guidance>",
		"Tautline local runtime instructions below are authoritative for Tautline tool names, workspace boundaries, safety, and execution workflow:\n" + workflow,
	}, "\n\n")
	return merged, status, nil
}

func loadCodexHostGuidance() (string, hostInstructionStatus, error) {
	status := hostInstructionStatus{Source: "tautline"}
	if disabledByEnvironment("TAUTLINE_CODEX_INSTRUCTIONS") {
		return "", status, nil
	}

	codexHome, err := resolveCodexHome()
	if err != nil {
		return "", status, err
	}
	var chunks []string
	var sources []string

	configPath := filepath.Join(codexHome, "config.toml")
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		rawPath, found, parseErr := parseCodexModelInstructionsFile(string(data))
		if parseErr != nil {
			return "", status, fmt.Errorf("parse Codex model_instructions_file: %w", parseErr)
		}
		if found {
			status.Configured = true
			instructionPath := expandInstructionPath(rawPath, codexHome)
			content, contentErr := readCodexGuidanceFile(instructionPath, "model_instructions_file")
			if contentErr != nil {
				return "", status, contentErr
			}
			if content == "" {
				return "", status, errors.New("Codex model_instructions_file is empty")
			}
			chunks = append(chunks, "<model_instructions_file>\n"+content+"\n</model_instructions_file>")
			sources = append(sources, "model_instructions_file")
			status.Bytes += len(content)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", status, fmt.Errorf("read Codex config: %w", readErr)
	}

	agentsPath, agentsName := codexGlobalAgentsPath(codexHome)
	if agentsPath != "" {
		status.Configured = true
		content, contentErr := readCodexGuidanceFile(agentsPath, agentsName)
		if contentErr != nil {
			return "", status, contentErr
		}
		if content != "" {
			chunks = append(chunks, "<global_agents file=\""+agentsName+"\">\n"+content+"\n</global_agents>")
			sources = append(sources, agentsName)
			status.Bytes += len(content)
		}
	}

	if len(chunks) == 0 {
		return "", status, nil
	}
	if status.Bytes > maxCodexModelInstructionsBytes {
		return "", status, fmt.Errorf("combined Codex host guidance exceeds %d bytes", maxCodexModelInstructionsBytes)
	}
	status.Loaded = true
	status.Source = "codex:" + strings.Join(sources, "+")
	return strings.Join(chunks, "\n\n"), status, nil
}

func codexGlobalAgentsPath(codexHome string) (string, string) {
	for _, name := range []string{"AGENTS.override.md", "AGENTS.md"} {
		path := filepath.Join(codexHome, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return path, name
		}
	}
	return "", ""
}

func readCodexGuidanceFile(path, label string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("open Codex %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Codex %s is not a regular file", label)
	}
	if info.Size() > maxCodexModelInstructionsBytes {
		return "", fmt.Errorf("Codex %s exceeds %d bytes", label, maxCodexModelInstructionsBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Codex %s: %w", label, err)
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return "", fmt.Errorf("Codex %s must be valid UTF-8 text", label)
	}
	return strings.TrimSpace(string(content)), nil
}

func resolveCodexHome() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
		return filepath.Abs(expandEnvironmentPath(configured))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for Codex: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

func parseCodexModelInstructionsFile(content string) (string, bool, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	section := ""
	for scanner.Scan() {
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		if section != "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "model_instructions_file" {
			continue
		}
		decoded, err := decodeTOMLString(strings.TrimSpace(value))
		if err != nil {
			return "", false, err
		}
		if strings.TrimSpace(decoded) == "" {
			return "", false, errors.New("model_instructions_file is empty")
		}
		return decoded, true, nil
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func stripTOMLComment(line string) string {
	var quote rune
	escaped := false
	for index, current := range line {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && current == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '"' || current == '\'' {
			quote = current
			continue
		}
		if current == '#' {
			return line[:index]
		}
	}
	return line
}

func decodeTOMLString(value string) (string, error) {
	if len(value) < 2 {
		return "", errors.New("model_instructions_file must be a quoted TOML string")
	}
	switch value[0] {
	case '"':
		if value[len(value)-1] != '"' {
			return "", errors.New("unterminated basic TOML string")
		}
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return decoded, nil
	case '\'':
		if value[len(value)-1] != '\'' {
			return "", errors.New("unterminated literal TOML string")
		}
		return value[1 : len(value)-1], nil
	default:
		return "", errors.New("model_instructions_file must be a quoted TOML string")
	}
}

func expandInstructionPath(value, codexHome string) string {
	expanded := expandEnvironmentPath(strings.TrimSpace(value))
	if expanded == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = home
		}
	} else if strings.HasPrefix(expanded, "~"+string(os.PathSeparator)) || strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(codexHome, expanded)
	}
	return filepath.Clean(expanded)
}

func expandEnvironmentPath(value string) string {
	expanded := os.ExpandEnv(value)
	for start := strings.IndexByte(expanded, '%'); start >= 0; start = strings.IndexByte(expanded, '%') {
		endOffset := strings.IndexByte(expanded[start+1:], '%')
		if endOffset < 0 {
			break
		}
		end := start + 1 + endOffset
		name := expanded[start+1 : end]
		replacement, exists := os.LookupEnv(name)
		if !exists {
			break
		}
		expanded = expanded[:start] + replacement + expanded[end+1:]
	}
	return expanded
}

func disabledByEnvironment(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "no", "off", "disabled":
		return true
	default:
		return false
	}
}
