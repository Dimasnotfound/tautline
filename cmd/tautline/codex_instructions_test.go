package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexModelInstructionsFile(t *testing.T) {
	value, found, err := parseCodexModelInstructionsFile(`
model = "gpt-test"
model_instructions_file = "C:\\prompts\\codex.md" # keep this comment out

[projects."C:\\repo"]
model_instructions_file = "ignored.md"
`)
	if err != nil {
		t.Fatalf("parse Codex config: %v", err)
	}
	if !found || value != `C:\prompts\codex.md` {
		t.Fatalf("value=%q found=%t", value, found)
	}
}

func TestHostInstructionsMergesCodexFileWithoutReplacingTautlineRules(t *testing.T) {
	codexHome := t.TempDir()
	instructionPath := filepath.Join(codexHome, "model.md")
	if err := os.WriteFile(instructionPath, []byte("Understand the repository before editing."), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `model_instructions_file = "model.md"` + "\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("TAUTLINE_CODEX_INSTRUCTIONS", "on")

	instructions, status, err := hostInstructions()
	if err != nil {
		t.Fatalf("build host instructions: %v", err)
	}
	if !status.Configured || !status.Loaded || status.Source != "codex:model_instructions_file" {
		t.Fatalf("unexpected host instruction status: %+v", status)
	}
	for _, expected := range []string{
		"Understand the repository before editing.",
		"Tautline local runtime instructions",
		"skills_search",
		"workspace_lookup",
	} {
		if !strings.Contains(instructions, expected) {
			t.Fatalf("merged instructions are missing %q", expected)
		}
	}
}

func TestHostInstructionsLoadsGlobalCodexAgentsWithoutModelFile(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "AGENTS.md"), []byte("Inspect the repository before changing files."), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("TAUTLINE_CODEX_INSTRUCTIONS", "on")

	instructions, status, err := hostInstructions()
	if err != nil {
		t.Fatalf("build host instructions: %v", err)
	}
	if !status.Configured || !status.Loaded || status.Source != "codex:AGENTS.md" {
		t.Fatalf("unexpected global AGENTS status: %+v", status)
	}
	if !strings.Contains(instructions, "Inspect the repository before changing files.") {
		t.Fatal("global Codex AGENTS.md was not merged")
	}
}

func TestHostInstructionsFallsBackWhenCodexFileIsMissing(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(`model_instructions_file = "missing.md"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("TAUTLINE_CODEX_INSTRUCTIONS", "on")

	instructions, status, err := hostInstructions()
	if err == nil {
		t.Fatal("missing Codex instruction file did not report an error")
	}
	if !status.Configured || status.Loaded {
		t.Fatalf("unexpected fallback status: %+v", status)
	}
	if !strings.Contains(instructions, "skills_search") {
		t.Fatal("Tautline workflow was not preserved during fallback")
	}
}

func TestExpandInstructionPathSupportsWindowsEnvironmentSyntax(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TAUTLINE_PROMPT_ROOT", root)
	path := expandInstructionPath(`%TAUTLINE_PROMPT_ROOT%/host.md`, t.TempDir())
	want := filepath.Join(root, "host.md")
	if path != want {
		t.Fatalf("expanded path=%q, want %q", path, want)
	}
}
