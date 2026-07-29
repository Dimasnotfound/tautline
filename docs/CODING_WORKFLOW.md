# Tautline coding workflow

Tautline v2.1.0 uses a context-safe, task-oriented loop. The default `changes` widget mode avoids rendering a large custom card for every low-level operation.

## Standard loop

1. Match relevant installed Hermes skills before non-trivial work.
2. Call `open_workspace` once for each project folder.
3. Reuse the returned `workspace_id` for every later workspace call.
4. Search before reading large files.
5. Read only the evidence required for the current change.
6. Use `write` or `edit` for text modifications.
7. Use `bash` for tests, builds, Git inspection, and bounded terminal work.
8. Use `artifact_read` when command output is stored as a secure artifact.
9. Call `show_changes` exactly once after the final modification.
10. Run current repository quality gates before reporting completion.

## Workspace rules

- Use paths relative to the opened workspace.
- Do not repeatedly open the same project.
- Do not access paths outside `TAUTLINE_ALLOWED_ROOTS`.
- Prefer search results and line windows over repeatedly loading complete large files.
- Preserve freshness hashes when an edit depends on an earlier read.
- Do not use shell redirection for text edits when `write` or `edit` is available.

## Widget modes

```env
TAUTLINE_WIDGETS=changes
```

- `changes` enables the compact workspace card and one final aggregate review.
- `full` enables workspace, file, diff, command, and change-review widgets.
- `off` disables custom widgets while preserving readable text output.

## Sub-agent workflow

Use sub-agents only for independent work that benefits from parallel analysis.

1. Call `list_subagents` and inspect the global enabled state, capacity, default model, and allowed models.
2. Choose a model from the returned allowlist.
3. Call `delegate_task` with complete instructions and the active `workspace_id` for read-only workspace access.
4. Poll the same `run_id` with `get_agent_run`.
5. Use `cancel_agent_run` when that exact run must stop.
6. Review delegated output before applying any repository modification.

New delegation is rejected when the global switch is off, no enabled slot is available, or the requested model is outside the allowlist. Tautline also rejects a completed result when 9Router reports an actual model outside the allowlist.

Do not create replacement runs merely because an active run is slow. Image tasks require an image-enabled slot, `requires_images=true`, and explicitly verified model capability.

## Change review

`show_changes` is an aggregate checkpoint, not a substitute for testing. Call it once after the final write or edit so the user can review the complete repository delta.

After the review card appears, run the project-specific format, test, lint, vet, type-check, and build commands on the current working tree. Do not rely on stale output produced before the latest edit.
