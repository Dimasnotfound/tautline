# Context-safe coding workflow

Tautline v2.5.2 uses one new prompt-scoped activity monitor per user turn, bounded tool results, and a dedicated launcher that isolates older widgets from later activity. The recommended workflow keeps repository context local and sends only the information needed for the current task.

## 1. Start one monitor per user turn

At the beginning of every user turn that uses MyLocal or Tautline, call `tautline_activity` exactly once before any other Tautline tool. Call it even when the conversation already contains older Tautline widgets, but never call it more than once in the same user turn. The call creates a unique prompt monitor and archives the monitor from the previous prompt.

## 2. Reuse or open one workspace

When a project may already be open, call `workspace_lookup` first. Reuse its `workspace_id` when found. Call `open_workspace` only once when lookup reports that the project is not open, then reuse that `workspace_id` for every later filesystem and command operation. Both tools are data-only, and the current prompt monitor binds to the selected workspace automatically.

## 3. Search before reading

Use `search` to find exact symbols, filenames, errors, or configuration keys. Read only the relevant line windows instead of loading complete large files.

## 4. Apply scoped changes

- Use `edit` for one exact, unique replacement.
- Use `write` for a complete file creation or intentional full replacement.
- Use `bash` for bounded commands, builds, tests, and file operations that cannot be expressed safely through `edit` or `write`.

## 5. Handle large output safely

Small command output remains inline. Large output is redacted and stored as a secure artifact. Use `artifact_read` to inspect only relevant ranges or search matches.

## 6. Use skills before non-trivial work

Call `skills_search` with the resolved task, then load a relevant compatible result through `skill_view`. Skill files remain read-only and secret-redacted.

## 7. Delegate only when useful

Sub-agent tasks require an enabled slot and an allowed 9Router model. Delegated workspace access is read-only. Inspect the result with `get_agent_run` before applying any proposed change.

## 8. Verify the result

Run the repository's existing formatting, tests, static checks, and build commands. Do not claim completion when a required verification step failed or was skipped.

## 9. Review aggregate changes

After the final file modification, call `show_changes` exactly once. Use the resulting review to confirm that only intended files changed and private configuration remains untracked.

## Activity monitor behavior

`tautline_activity` is the only render tool. Each call returns a unique `monitor_id` for one user prompt. The widget sends that identifier to the app-only `activity_snapshot` tool on every poll, preventing an older widget from reading another prompt's activity. When the next prompt starts, the previous monitor becomes archived and its widget stops polling automatically. Archived timelines remain inspectable.

Selecting an older event pins the inspector to that event while new activity continues to arrive in the active prompt monitor. The **Latest** button clears the pinned selection, scrolls the timeline to the newest event, and resumes automatic tracking. `open_workspace`, `workspace_lookup`, and every workspace, command, skill, agent, browser, or external-MCP action remain data-only.
