# Context-safe coding workflow

Tautline v2.4.0 uses one live activity monitor and bounded tool results. The recommended workflow keeps repository context local and sends only the information needed for the current task.

## 1. Open one workspace

Call `open_workspace` with an allowed project directory. Reuse the returned `workspace_id` for every later filesystem and command operation.

## 2. Search before reading

Use `search` to find exact symbols, filenames, errors, or configuration keys. Read only the relevant line windows instead of loading complete large files.

## 3. Apply scoped changes

- Use `edit` for one exact, unique replacement.
- Use `write` for a complete file creation or intentional full replacement.
- Use `bash` for bounded commands, builds, tests, and file operations that cannot be expressed safely through `edit` or `write`.

## 4. Handle large output safely

Small command output remains inline. Large output is redacted and stored as a secure artifact. Use `artifact_read` to inspect only relevant ranges or search matches.

## 5. Use skills before non-trivial work

Call `skills_search` with the resolved task, then load a relevant compatible result through `skill_view`. Skill files remain read-only and secret-redacted.

## 6. Delegate only when useful

Sub-agent tasks require an enabled slot and an allowed 9Router model. Delegated workspace access is read-only. Inspect the result with `get_agent_run` before applying any proposed change.

## 7. Verify the result

Run the repository's existing formatting, tests, static checks, and build commands. Do not claim completion when a required verification step failed or was skipped.

## 8. Review aggregate changes

After the final file modification, call `show_changes` exactly once. Use the resulting review to confirm that only intended files changed and private configuration remains untracked.

## Activity monitor behavior

`open_workspace` owns the single widget resource. Later workspace, command, skill, agent, browser, and external-MCP actions update the same activity timeline and inspector without mounting additional widgets.
