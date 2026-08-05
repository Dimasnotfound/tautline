package main

import (
	"strings"
	"testing"
	"time"
)

func TestProcessSessionAcceptsInputAndReturnsIncrementalOutput(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{root})
	workspace := registerWorkspace(root)
	manager := newProcessSessionManager()
	t.Cleanup(manager.shutdown)

	started, err := manager.start(
		workspace.ID,
		root,
		`printf 'ready\n'; IFS= read -r line; printf 'echo:%s\n' "$line"`,
		500,
		defaultProcessOutputBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !started.Running || started.SessionID == "" {
		t.Fatalf("interactive command did not stay running: %+v", started)
	}
	if !strings.Contains(started.Output, "ready") {
		polled, pollErr := manager.interact(workspace.ID, started.SessionID, "", false, false, false, 1_000, defaultProcessOutputBytes)
		if pollErr != nil {
			t.Fatal(pollErr)
		}
		started.Output += "\n" + polled.Output
	}
	if !strings.Contains(started.Output, "ready") {
		t.Fatalf("initial process output was not returned: %+v", started)
	}

	finished, err := manager.interact(
		workspace.ID,
		started.SessionID,
		"hello\n",
		true,
		false,
		false,
		5_000,
		defaultProcessOutputBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Running {
		finished, err = manager.interact(workspace.ID, started.SessionID, "", false, false, false, 5_000, defaultProcessOutputBytes)
		if err != nil {
			t.Fatal(err)
		}
	}
	if finished.Running || finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Fatalf("interactive process did not finish successfully: %+v", finished)
	}
	if !strings.Contains(finished.Output, "echo:hello") {
		t.Fatalf("process did not receive stdin: %+v", finished)
	}
}

func TestProcessSessionIsWorkspaceScopedAndTerminatesTree(t *testing.T) {
	rootA, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolateWorkspaceRegistry(t, []string{rootA, rootB})
	workspaceA := registerWorkspace(rootA)
	workspaceB := registerWorkspace(rootB)
	manager := newProcessSessionManager()
	t.Cleanup(manager.shutdown)

	started, err := manager.start(
		workspaceA.ID,
		rootA,
		`printf 'started\n'; while :; do sleep 1; done`,
		300,
		defaultProcessOutputBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !started.Running || started.SessionID == "" {
		t.Fatalf("long-running command did not return a session: %+v", started)
	}
	if _, err := manager.interact(workspaceB.ID, started.SessionID, "", false, false, false, 1, defaultProcessOutputBytes); err == nil {
		t.Fatal("process session was accessible from another workspace")
	}

	finished, err := manager.interact(
		workspaceA.ID,
		started.SessionID,
		"",
		false,
		false,
		true,
		5_000,
		defaultProcessOutputBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for finished.Running && time.Now().Before(deadline) {
		finished, err = manager.interact(workspaceA.ID, started.SessionID, "", false, false, false, 500, defaultProcessOutputBytes)
		if err != nil {
			t.Fatal(err)
		}
	}
	if finished.Running || finished.ExitCode == nil || *finished.ExitCode == 0 {
		t.Fatalf("terminated process session did not report a non-zero exit: %+v", finished)
	}
}

func TestProcessOutputBufferBoundsAndRedacts(t *testing.T) {
	buffer := &processOutputBuffer{limit: 32}
	_, _ = buffer.Write([]byte("prefix SECRET_KEY=should-not-leak suffix that exceeds the limit"))
	output, truncated, redacted := buffer.drain(128)
	if !truncated {
		t.Fatal("bounded process output did not report truncation")
	}
	if strings.Contains(output, "should-not-leak") {
		t.Fatalf("secret-looking process output was not redacted: %q", output)
	}
	if !redacted && strings.Contains(output, "SECRET_KEY") {
		t.Fatalf("redaction status was not reported: %q", output)
	}
}
