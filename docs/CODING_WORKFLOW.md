# Codex-style Coding Workflow

DevSpace v1.4 uses a task-oriented loop instead of rendering a custom widget for every low-level tool call.

## Default loop

1. Call `open_workspace` once with an absolute project directory.
2. Reuse the returned `workspace_id` for the rest of the task.
3. Pass relative paths to `read`, `write`, and `edit`.
4. Use `bash` for inspection, tests, builds, Git, and package scripts.
5. Make file changes through `write` or `edit` so DevSpace can track the original content.
6. After the final file change, call `show_changes` exactly once.
7. Run verification and provide a short final response.

Example:

```text
open_workspace(path="D:\Projects\demo")
→ workspace_id: ws_81cce3c914d0

read(workspace_id="ws_81cce3c914d0", path="src/main.go")
edit(workspace_id="ws_81cce3c914d0", path="src/main.go", ...)
bash(workspace_id="ws_81cce3c914d0", command="go test ./...")
show_changes(workspace_id="ws_81cce3c914d0")
```

## Widget modes

| Mode | Custom widgets | Use case |
|---|---|---|
| `changes` | Workspace open + final aggregate review | Recommended, clean coding workflow |
| `full` | Workspace, file, individual diff, and command | Debugging or users who want every visual card |
| `off` | None | Plain MCP clients or minimal ChatGPT UI |

Configure the mode in `.env`:

```dotenv
DEVSPACE_WIDGETS=changes
```

## Review checkpoints

The first `write` or `edit` to a file stores its original content for the current checkpoint. Further edits to the same file preserve that same original snapshot. `show_changes` compares every tracked file against its current content, renders one aggregate diff, and advances the checkpoint.

Calling `show_changes` a second time without another modification returns `No pending changes`.

## Important limitation

Changes made through arbitrary shell commands are not automatically tracked by the in-memory review checkpoint. Use `write` and `edit` for file modifications. Shell commands should be used for verification and project tooling.
