<p align="center">
  <img src="assets/tautline.svg" alt="Tautline logo" width="104" />
</p>

<h1 align="center">Tautline</h1>

<p align="center">
  <strong>A context-safe, local-first MCP control plane for ChatGPT.</strong><br />
  Workspace tools, prompt-scoped activity monitors, external MCP integrations, controlled sub-agents, Lightpanda browsing, and Cloudflare Tunnel in one Go runtime.
</p>

<p align="center">
  <a href="https://github.com/Dimasnotfound/devspace-mcp/actions/workflows/ci.yml"><img alt="CI status" src="https://github.com/Dimasnotfound/devspace-mcp/actions/workflows/ci.yml/badge.svg" /></a>
  <img alt="Tautline version 2.10.0" src="https://img.shields.io/badge/version-2.10.0-38bdf8" />
  <img alt="Go 1.25.5 or newer" src="https://img.shields.io/badge/Go-1.25.5%2B-00ADD8" />
  <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/badge/license-MIT-818cf8" /></a>
</p>

## Overview

Tautline lets ChatGPT work with explicitly allowed local project folders without uploading an entire repository into a conversation. It provides bounded file and command tools, secure storage for large command output, a local dashboard, ordinary ChatGPT relay workers, optional legacy 9Router agents, native Lightpanda browser tools, Cloudflare Tunnel support, and a generic MCP client that can republish tools from other MCP servers.

Version 2.10.0 adds the optional Laju Relay Bridge. A main conversation creates a task and Tautline queues its worker prompt; the Laju extension opens a fresh ordinary ChatGPT tab, submits the prompt through the visible composer, and the worker claims, performs, updates, and completes the task through Tautline. Manual `worker_prompt` delivery remains available when the bridge is disconnected. The bridge uses no ChatGPT account token, private backend endpoint, Codex process, OpenAI API model call, 9Router request, Playwright, Selenium, Electron, or additional Chromium runtime.

## Main capabilities

| Area | Capability |
|---|---|
| Workspace | Reuse existing checkouts or create managed detached Git worktrees, search files, read one bounded window or up to ten files with `read_many`, write atomically, edit exact text, and review aggregate changes. |
| Commands | Use `bash` for bounded one-shot commands or `exec_command` plus `write_stdin` for long-running pipe-backed sessions with polling, stdin, interruption, and process-tree termination. |
| Diagnostics | Run `tautline_doctor` or `tautline -doctor` for a read-only runtime, integration, security, and version summary with concrete corrective actions. |
| Context safety | Keep normal responses compact and place oversized command output in secure, redacted local artifacts. |
| Activity monitor | Create one isolated live widget per user prompt, archive older monitors, and inspect or resume the latest activity. |
| MCP integrations | Connect through `stdio`, Streamable HTTP, legacy SSE, or automatic HTTP-to-SSE fallback and republish tools with unique prefixes. |
| Google Docs | Read and update documents through native Go tools backed by the stable Google Docs REST API, with local OAuth refresh and no Node.js runtime. |
| Sub-agents | Delegate through ordinary ChatGPT relay worker conversations with optional automatic Laju fresh-tab handoff, one-time join codes, private worker tokens, progress reporting, cancellation, timeouts, manual fallback, and optional legacy 9Router execution. |
| Browser | Use Lightpanda native browser tools with optional persistent local session state. |
| Dashboard | Configure integrations, agents, browser runtime, tunnel settings, and activity locally. |

## Requirements

- Go 1.25.5 or newer.
- Python 3 with Pillow for Windows icon generation.
- Node.js 22 or newer for JavaScript validation.
- Git.
- Windows PowerShell 5.1 or newer for embedding the application icon.
- Optional: Laju Browser for automatic relay handoff, legacy 9Router, Lightpanda through WSL2 or Docker, and `cloudflared`. ChatGPT relay agents require no extra model runtime.

## Windows quick start

Clone the repository into a folder named `tautline`:

```powershell
git clone https://github.com/Dimasnotfound/devspace-mcp.git tautline
cd tautline
```

Create private configuration for a new installation:

```powershell
./scripts/setup.ps1 -AllowedRoots "D:\Projects"
```

Build the Windows executables:

```powershell
./scripts/build.ps1
```

Start Tautline:

```powershell
./scripts/start.ps1
```

You can also double-click `START_TAUTLINE.cmd`. The main executable is created at:

```text
bin\tautline.exe
```

The executable contains the current Tautline application icon. The optional Lightpanda bridge is created at `bin\lightpanda-shim.exe`.

When replacing an older instance, double-click `SWITCH_TO_TAUTLINE.cmd`. It runs all quality gates, starts the staged v2.10.0 binary on an isolated temporary port, verifies the Relay Bridge health contract, native Google Docs activation, configured external MCP integrations, and a fresh ChatGPT OAuth connection flow, and only then stops the old runtime for an atomic handoff. After a successful handoff it installs the optional Laju extension without restarting Laju Browser. If activation fails, the previous binaries are restored.

## Existing local installation migration

For a private machine migration, preserve only the state still required by the new workspace:

```text
.env
.owner_token.txt
runtime/v2/config/tautline.json
runtime/v2/oauth/
runtime/v2/state/agents.json
runtime/v2/state/oauth-clients.json
runtime/v2/state/dashboard-admin.key
```

Do not migrate generated artifacts, logs, PID files, caches, temporary command output, old binaries, or Lightpanda virtual disks into the repository. These files are recreated when required.

Private files and all runtime state are excluded by `.gitignore`. Never commit them.

## Default endpoints

| Component | Default |
|---|---|
| Dashboard | `http://127.0.0.1:7688/` |
| MCP | `http://127.0.0.1:7688/mcp` (existing apps) or `http://127.0.0.1:7688/mcp/v2` (fresh/cache-busting apps) |
| Health | `http://127.0.0.1:7688/healthz` |
| Legacy 9Router | `http://127.0.0.1:20128/v1` when `TAUTLINE_AGENT_BACKEND=9router` |
| Lightpanda CDP | `http://127.0.0.1:9223` |
| Runtime directory | `runtime/v2/` |
| Activity widget | `ui://tautline/activity-v6.html` |

Run a read-only local diagnostic at any time:

```powershell
bin\tautline.exe -doctor
```

The command exits non-zero when it detects an error such as an offline runtime, disabled MCP authentication, invalid allowed roots, or an active binary version that does not match the source build.

## Configuration

Copy [`.env.example`](.env.example) to `.env` or use `scripts/setup.ps1`. Important settings include:

| Variable | Purpose |
|---|---|
| `TAUTLINE_ALLOWED_ROOTS` | Comma-separated folders Tautline may access. Keep this list narrow. |
| `TAUTLINE_OWNER_TOKEN` | Secret used during OAuth owner authorization. |
| `TAUTLINE_REQUIRE_AUTH` | Keep `true` whenever the MCP endpoint is exposed. |
| `TAUTLINE_RUNTIME_DIR` | Local runtime configuration and state directory. |
| `TAUTLINE_WORKTREE_ROOT` | Directory for Tautline-managed detached Git worktrees. Defaults to `<runtime>/worktrees`. |
| `TAUTLINE_WIDGETS` | `on` or `off`. Legacy `changes` and `full` values are treated as `on`. |
| `TAUTLINE_CODEX_INSTRUCTIONS` | Set to `off` to disable loading Codex `model_instructions_file`; enabled by default. |
| `TAUTLINE_PUBLIC_BASE_URL` | Public HTTPS origin used by OAuth metadata. |
| `TAUTLINE_WIDGET_DOMAIN` | Stable HTTPS origin declared for MCP Apps. |
| `TAUTLINE_AGENT_BACKEND` | `chatgpt-relay` by default, or `9router` for the optional legacy model endpoint. |
| `TAUTLINE_9ROUTER_ALLOWED_MODELS` | Comma-separated allowlist used only by the legacy `9router` backend. |
| `TAUTLINE_LIGHTPANDA_PATH` | `auto`, `docker`, `wsl`, or an executable path. |
| `TAUTLINE_TUNNEL_MODE` | `off`, `quick`, or `named`. |

Dashboard changes are saved atomically to `runtime/v2/config/tautline.json`. Environment variables override stored configuration at startup.

### ChatGPT Relay Agents

The default `chatgpt-relay` backend coordinates work between ordinary ChatGPT conversations without making a model request itself. The main conversation calls `delegate_task`; Tautline queues the returned `worker_prompt` for the optional Laju Relay Bridge. When connected, the extension opens a fresh `chatgpt.com` tab and submits the prompt through the visible composer. The worker calls `claim_agent_task`, performs the returned task with the returned workspace, reports meaningful progress through `update_agent_run`, and calls `complete_agent_task` exactly once before its final response. The main conversation inspects the result through `get_agent_run` or cancels it through `cancel_agent_run`.

Install the extension with `INSTALL_LAJU_RELAY_BRIDGE.cmd`, then restart Laju Browser once. `SWITCH_TO_TAUTLINE.cmd` also installs or refreshes the extension after a successful v2.10 handoff. When the bridge is disconnected, `worker_prompt` remains available for the original manual New Chat flow. The extension has no `<all_urls>` access and is limited to `https://chatgpt.com/*` plus the loopback Tautline endpoint. It never reads ChatGPT cookies, account tokens, or private backend endpoints. Join codes remain random and single-use; private worker tokens stay in memory and are cleared at terminal states. Images are still manual-only.

### Managed Git worktrees

`open_workspace` defaults to `mode=checkout`, which opens the user's existing directory and should be reused through `workspace_lookup`. For isolated or parallel work, use `mode=worktree` with an optional `base_ref`. Tautline resolves the requested commit, reports whether the source checkout has uncommitted changes, and creates a new detached worktree under `TAUTLINE_WORKTREE_ROOT` without copying those uncommitted changes. Worktree metadata is persisted in `runtime/v2/state/workspaces.json` and restored only while both the allowed source repository and managed worktree boundary remain valid.

### Interactive process sessions

`bash` remains the bounded one-shot command tool. Use `exec_command` when a command may outlive one MCP call or needs later stdin. A still-running command returns a workspace-scoped `session_id`; pass it to `write_stdin` to poll incremental output, send text, close stdin, request Ctrl-C on Unix or Ctrl-Break on Windows, or terminate the process tree. Sessions are pipe-backed rather than full PTYs, retain at most 1 MiB of pending output, redact secret-looking values before returning them, and are terminated when Tautline shuts down.

### ChatGPT OAuth registration

A fresh ChatGPT/OpenAI connection receives the same public `client_id` contract used by the working v2.4 releases, while exact trusted loopback callbacks receive unique dynamic client IDs persisted in `runtime/v2/state/oauth-clients.json`. All flows require PKCE S256, advertise `tautline offline_access`, and bind new tokens to either `/mcp` or `/mcp/v2`. Tautline serves Protected Resource Metadata, Authorization Server Metadata, and OIDC-compatible discovery aliases at the standard locations and the additional path variants used by ChatGPT scanners. Unauthenticated requests advertise endpoint-specific metadata through `WWW-Authenticate`. Use `/mcp/v2` when a failed `/mcp` application may still be held in a negative discovery cache.

### Codex host instructions

At startup, Tautline checks `$CODEX_HOME/config.toml`, or `%USERPROFILE%\.codex\config.toml` when `CODEX_HOME` is unset. It loads the top-level `model_instructions_file` when configured, then reuses the first non-empty global `AGENTS.override.md` or `AGENTS.md` from the same Codex directory. This guidance is merged into `server.WithInstructions(...)` for the primary ChatGPT MCP connection, while Tautline's local tool, workspace, and safety rules remain authoritative. Invalid explicit configuration never prevents normal startup, and this loader does not modify sub-agent prompts.

## Prompt-scoped activity monitors

Tautline registers one reusable MCP App resource:

```text
ui://tautline/activity-v6.html
```

For non-trivial turns, `skills_search` owns the output template and creates a unique prompt `monitor_id` before recording its own activity. This guarantees a fresh widget even when the host skips a separate launcher call. For a trivial status check or direct workspace request that skips skill matching, `tautline_activity` remains the explicit prompt launcher. The two tools must not be called in the same user turn. `open_workspace`, `workspace_lookup`, and every other tool remain data-only, while the app-only `activity_snapshot` requires the widget's `monitor_id`.

Only the newest prompt monitor receives new events. When another prompt begins, the previous monitor becomes archived and its widget stops automatic polling. Archived widgets remain usable for inspecting their existing timeline, but they cannot display activity from later prompts. The **Latest** button exits a pinned historical selection, returns the timeline to the newest event, and resumes automatic tracking while the monitor is active.

The monitor updates its static shell incrementally, preserves panel scroll positions while inspecting history, caches a bounded number of details, and slows polling automatically when activity is unchanged. Open workspace paths remain persisted locally in `runtime/v2/state/workspaces.json`. Event payloads are redacted and size-limited, prompt monitor IDs are random, and all in-memory activity is cleared when Tautline stops.

## External MCP integrations

Tautline supports:

- Local child-process integrations through `stdio`.
- Local or remote Streamable HTTP MCP endpoints.
- Legacy HTTP+SSE endpoints for backward compatibility.
- `auto` transport, which tries Streamable HTTP first and falls back to SSE only for compatible protocol failures.
- OAuth-enabled URL integrations and static headers.
- Environment placeholders such as `${VARIABLE_NAME}` for secret values.

Existing `transport: "http"` configuration is normalized to `streamable-http` automatically. Endpoint paths are otherwise preserved. The former official Google Docs MCP connector is migrated separately into Tautline's native REST integration.

Remote plain HTTP endpoints are rejected unless the host is loopback. Connector secrets are resolved only when the connector starts and are not returned by the dashboard or activity monitor.

### Google Docs

Tautline v2.7.0 publishes `gdocs_read_doc` and `gdocs_update_doc` directly from the Go runtime and calls:

```text
GET  https://docs.googleapis.com/v1/documents/{documentId}
POST https://docs.googleapis.com/v1/documents/{documentId}:batchUpdate
```

On first startup, an existing `google_docs` connector that targets `docsmcp.googleapis.com` is migrated into the top-level `google_docs` configuration. Its OAuth client, scopes, timeout, and token file are preserved, while the preview connector is disabled to prevent duplicate tool names.

Authorize or replace the stored token with:

```powershell
bin\tautline.exe -auth-google-docs
```

Verify real document access without modifying it:

```powershell
bin\tautline.exe -test-google-docs DOCUMENT_ID
```

The OAuth token is stored under `runtime/v2/oauth/`, refreshed automatically, and ignored by Git. Native Google Docs requires the regular Google Docs API to be enabled in the selected Google Cloud project; it does not require `docsmcp.googleapis.com`, Node.js, or Google Workspace Developer Preview access.

## Build and verification

Windows:

```powershell
./scripts/build.ps1
```

macOS or Linux:

```bash
./scripts/build.sh
```

The complete quality gate performs:

```text
gofmt validation
Python syntax validation
Dashboard JavaScript syntax validation
go test -count=1 ./...
go vet ./...
Go builds for Tautline and the Lightpanda shim
```

## Repository layout

```text
.
├── .github/workflows/       # CI and secret scanning
├── assets/                  # Current Tautline logo and Windows icon
├── cmd/
│   ├── tautline/            # Tautline v2.10.0 source and embedded web assets
│   └── lightpanda-shim/     # Optional Windows-to-WSL Lightpanda shim
├── docs/                    # Setup and coding workflow guides
├── runtime/                 # Ignored machine-local state; only .gitkeep is tracked
├── scripts/                 # Setup, build, start, stop, and icon helpers
├── .env.example             # Safe configuration template
├── go.mod                   # Go module definition
└── README.md                # Project overview and usage
```

## Security

Tautline can modify files and execute commands inside configured roots. Treat it as privileged development software.

- Keep `TAUTLINE_ALLOWED_ROOTS` limited to trusted project directories.
- Never expose `/mcp` with authentication disabled.
- Never commit `.env`, owner tokens, OAuth tokens, tunnel credentials, runtime state, logs, or executables.
- Keep the dashboard bound to loopback.
- Review requested operations and delegated output before applying changes.

See [SECURITY.md](SECURITY.md) for the disclosure policy.

## License

Tautline is released under the [MIT License](LICENSE).

Tautline is an independent open-source project and is not affiliated with or endorsed by OpenAI, Google, Cloudflare, Lightpanda, or any model provider. Their names and trademarks belong to their respective owners.
