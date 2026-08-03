# Context-safe coding workflow

Tautline v2.5.1 uses one live activity monitor, bounded tool results, and a dedicated first-use launcher that prevents duplicate widget cards. The recommended workflow keeps repository context local and sends only the information needed for the current task.

## 1. Activate the monitor once

At the first MyLocal or Tautline interaction in a conversation, call `tautline_activity` exactly once. It is the only tool that owns widget output metadata. Skip it when the conversation already contains the Tautline activity widget.

## 2. Reuse or open one workspace

When a project may already be open, call `workspace_lookup` first. Reuse its `workspace_id` when found. Call `open_workspace` only once when lookup reports that the project is not open, then reuse that `workspace_id` for every later filesystem and command operation. Both tools are data-only; the existing monitor follows the active workspace automatically.

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

`tautline_activity` is the only render tool. `open_workspace`, `workspace_lookup`, `activity_snapshot`, and every workspace, command, skill, agent, browser, or external-MCP action are data-only. The widget polls `activity_snapshot` without a required `workspace_id`, restores the active persisted workspace, and follows later workspace changes without remounting its iframe. The monitor updates its static shell incrementally, keeps the timeline and preview independently scrollable, and reuses cached details for faster repeat selection.
