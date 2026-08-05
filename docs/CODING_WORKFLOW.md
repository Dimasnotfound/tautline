# Context-safe coding workflow

Tautline v2.9.0 creates one new prompt-scoped activity monitor at the first prompt-boundary tool in each user turn, while retaining managed checkout or worktree workspaces, ChatGPT relay workers, and bounded single-call or interactive command execution. Multi-file reads, read-only diagnostics, native Google Docs REST tools, Codex host guidance, multi-transport MCP support, and isolated release preflight continue to protect the active runtime.

## 1. Start one monitor per user turn

For every non-trivial user turn that uses MyLocal or Tautline, call `skills_search` exactly once before any other Tautline tool. It creates and renders a unique prompt monitor before recording the skill search, so do not also call `tautline_activity` in that turn. For a trivial status check or direct workspace request that legitimately skips skill matching, call `tautline_activity` exactly once before other Tautline tools. Either prompt-boundary call archives the monitor from the previous prompt.

## 2. Reuse an existing checkout

When work should happen in the user's current checkout, call `workspace_lookup` first. Reuse its `workspace_id` when found. Call `open_workspace` in its default `checkout` mode only when lookup reports that the project is not open, then reuse that identifier for later filesystem and command operations.

## 3. Create an isolated worktree when needed

For parallel or isolated Git work, call `open_workspace` with `mode=worktree` and an optional `base_ref`. Every worktree call intentionally creates a new detached workspace under `TAUTLINE_WORKTREE_ROOT`; it does not copy uncommitted source-checkout changes. Continue with the returned worktree `workspace_id`, not the checkout identifier. Tautline reports the source root, resolved base commit, and whether the source checkout was dirty.

## 4. Search before reading

Use `search` to find exact symbols, filenames, errors, or configuration keys. Read only the relevant line windows instead of loading complete large files. When a small known set of files is required together, use `read_many` for at most ten bounded requests rather than issuing repeated full-file reads.

## 5. Apply scoped changes

- Use `edit` for one exact, unique replacement.
- Use `write` for a complete file creation or intentional full replacement.
- Use command tools for builds, tests, Git operations, and other terminal work rather than using shell redirection to replace file-edit tools.

## 6. Choose the correct command mode

Use `bash` for bounded one-shot commands that should complete within one call. Use `exec_command` for a long-running command or one that may require later input. When `exec_command` returns a `session_id`, call `write_stdin` with the same `workspace_id` to:

- poll incremental output;
- send UTF-8 input or close stdin;
- request Ctrl-C on Unix or Ctrl-Break on Windows;
- terminate the process tree.

Process sessions are pipe-backed, not full PTYs, and therefore do not support terminal resizing or applications that require a real interactive terminal screen.

## 7. Handle output safely

Large one-shot `bash` output is redacted and stored as a secure artifact; use `artifact_read` to inspect relevant ranges. Process-session output remains in a bounded in-memory incremental buffer, is redacted before it is returned, and reports when earlier bytes were omitted.

## 8. Use skills before non-trivial work

Call `skills_search` with the resolved task, then load a relevant compatible result through `skill_view`. Skill files remain read-only and secret-redacted.

## 9. Delegate only when useful

Sub-agent tasks require an enabled slot. With the default `chatgpt-relay` backend, `delegate_task` returns `worker_prompt`; ask the user to open an ordinary ChatGPT New Chat and paste it. That worker calls `claim_agent_task`, keeps `worker_token` private, works only on the returned task and workspace, reports meaningful progress through `update_agent_run`, and calls `complete_agent_task` exactly once before its final response. Inspect the result with `get_agent_run`. The optional `9router` backend still enforces its model allowlist.

## 10. Verify the result

Run the repository's existing formatting, tests, static checks, and build commands. Do not claim completion when a required verification step failed or was skipped. Worktree verification must be performed inside the returned worktree workspace so the user's source checkout remains isolated.

## 11. Review aggregate changes

After the final file modification, call `show_changes` exactly once. Use the resulting review to confirm that only intended files changed and private configuration remains untracked.

## Activity monitor behavior

`skills_search` is the render and prompt-boundary tool for non-trivial turns, while `tautline_activity` provides the same boundary for trivial or direct turns that skip skill matching. Each call returns a unique `monitor_id` for one user prompt. The widget sends that identifier to the app-only `activity_snapshot` tool on every poll, preventing an older widget from reading another prompt's activity. When the next prompt starts, the previous monitor becomes archived and its widget stops polling automatically. Archived timelines remain inspectable.

Selecting an older event pins the inspector to that event while new activity continues to arrive in the active prompt monitor. The **Latest** button clears the pinned selection, scrolls the timeline to the newest event, and resumes automatic tracking. `open_workspace`, `workspace_lookup`, and every workspace, command, skill, agent, browser, or external-MCP action remain data-only.
