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
  <img alt="Tautline version 2.6.0" src="https://img.shields.io/badge/version-2.6.0-38bdf8" />
  <img alt="Go 1.25.5 or newer" src="https://img.shields.io/badge/Go-1.25.5%2B-00ADD8" />
  <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/badge/license-MIT-818cf8" /></a>
</p>

## Overview

Tautline lets ChatGPT work with explicitly allowed local project folders without uploading an entire repository into a conversation. It provides bounded file and command tools, secure storage for large command output, a local dashboard, optional 9Router sub-agents, native Lightpanda browser tools, Cloudflare Tunnel support, and a generic MCP client that can republish tools from other MCP servers.

Version 2.6.0 loads optional host-level guidance from Codex `model_instructions_file` and merges it with Tautline's authoritative workspace and safety workflow for the primary ChatGPT connection only. ChatGPT/OpenAI OAuth registration retains the v2.4-compatible public client while trusted loopback clients use persisted dynamic registrations. Discovery supports the standard metadata locations, the path variants probed by ChatGPT, `offline_access`, resource-bound tokens, refresh-token rotation, and both `/mcp` and cache-busting `/mcp/v2` endpoints. External MCP integrations normalize legacy configuration and support `stdio`, Streamable HTTP, legacy SSE, and automatic HTTP-to-SSE fallback. The Windows switch launcher runs the staged binary on an isolated temporary port, verifies configured MCP connections plus a complete OAuth registration/PKCE/refresh/MCP flow, and only then stops the active version for an atomic handoff with rollback.

## Main capabilities

| Area | Capability |
|---|---|
| Workspace | Open configured roots, search files, read bounded windows, write atomically, edit exact text, execute bounded commands, and review aggregate changes. |
| Context safety | Keep normal responses compact and place oversized command output in secure, redacted local artifacts. |
| Activity monitor | Create one isolated live widget per user prompt, archive older monitors, and inspect or resume the latest activity. |
| MCP integrations | Connect through `stdio`, Streamable HTTP, legacy SSE, or automatic HTTP-to-SSE fallback and republish tools with unique prefixes. |
| Google Docs | Use Google's official remote Docs MCP endpoint after one-time OAuth authorization. |
| Sub-agents | Delegate controlled tasks through an OpenAI-compatible 9Router endpoint with model and capability allowlists. |
| Browser | Use Lightpanda native browser tools with optional persistent local session state. |
| Dashboard | Configure integrations, agents, browser runtime, tunnel settings, and activity locally. |

## Requirements

- Go 1.25.5 or newer.
- Python 3 with Pillow for Windows icon generation.
- Node.js 22 or newer for JavaScript validation.
- Git.
- Windows PowerShell 5.1 or newer for embedding the application icon.
- Optional: 9Router, Lightpanda through WSL2 or Docker, and `cloudflared`.

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

When replacing an older instance, double-click `SWITCH_TO_TAUTLINE.cmd`. It runs all quality gates, starts the staged v2.6.0 binary on an isolated temporary port, verifies configured MCP integrations and a fresh ChatGPT OAuth connection flow, and only then stops the old runtime for an atomic handoff. If activation fails, the previous binaries are restored.

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
| 9Router | `http://127.0.0.1:20128/v1` |
| Lightpanda CDP | `http://127.0.0.1:9223` |
| Runtime directory | `runtime/v2/` |
| Activity widget | `ui://tautline/activity-v4.html` |

## Configuration

Copy [`.env.example`](.env.example) to `.env` or use `scripts/setup.ps1`. Important settings include:

| Variable | Purpose |
|---|---|
| `TAUTLINE_ALLOWED_ROOTS` | Comma-separated folders Tautline may access. Keep this list narrow. |
| `TAUTLINE_OWNER_TOKEN` | Secret used during OAuth owner authorization. |
| `TAUTLINE_REQUIRE_AUTH` | Keep `true` whenever the MCP endpoint is exposed. |
| `TAUTLINE_RUNTIME_DIR` | Local runtime configuration and state directory. |
| `TAUTLINE_WIDGETS` | `on` or `off`. Legacy `changes` and `full` values are treated as `on`. |
| `TAUTLINE_CODEX_INSTRUCTIONS` | Set to `off` to disable loading Codex `model_instructions_file`; enabled by default. |
| `TAUTLINE_PUBLIC_BASE_URL` | Public HTTPS origin used by OAuth metadata. |
| `TAUTLINE_WIDGET_DOMAIN` | Stable HTTPS origin declared for MCP Apps. |
| `TAUTLINE_9ROUTER_ALLOWED_MODELS` | Comma-separated model allowlist for delegated tasks. |
| `TAUTLINE_LIGHTPANDA_PATH` | `auto`, `docker`, `wsl`, or an executable path. |
| `TAUTLINE_TUNNEL_MODE` | `off`, `quick`, or `named`. |

Dashboard changes are saved atomically to `runtime/v2/config/tautline.json`. Environment variables override stored configuration at startup.

### ChatGPT OAuth registration

A fresh ChatGPT/OpenAI connection receives the same public `client_id` contract used by the working v2.4 releases, while exact trusted loopback callbacks receive unique dynamic client IDs persisted in `runtime/v2/state/oauth-clients.json`. All flows require PKCE S256, advertise `tautline offline_access`, and bind new tokens to either `/mcp` or `/mcp/v2`. Tautline serves Protected Resource Metadata, Authorization Server Metadata, and OIDC-compatible discovery aliases at the standard locations and the additional path variants used by ChatGPT scanners. Unauthenticated requests advertise endpoint-specific metadata through `WWW-Authenticate`. Use `/mcp/v2` when a failed `/mcp` application may still be held in a negative discovery cache.

### Codex host instructions

At startup, Tautline checks `$CODEX_HOME/config.toml`, or `%USERPROFILE%\.codex\config.toml` when `CODEX_HOME` is unset. It loads the top-level `model_instructions_file` when configured, then reuses the first non-empty global `AGENTS.override.md` or `AGENTS.md` from the same Codex directory. This guidance is merged into `server.WithInstructions(...)` for the primary ChatGPT MCP connection, while Tautline's local tool, workspace, and safety rules remain authoritative. Invalid explicit configuration never prevents normal startup, and this loader does not modify sub-agent prompts.

## Prompt-scoped activity monitors

Tautline registers one reusable MCP App resource:

```text
ui://tautline/activity-v4.html
```

Only `tautline_activity` owns the output template. The server instructions direct ChatGPT to call it exactly once at the beginning of every user turn that uses Tautline, even when an older widget already exists. Each call creates a unique `monitor_id`. `open_workspace`, `workspace_lookup`, and every other tool remain data-only, while the app-only `activity_snapshot` requires the widget's `monitor_id`.

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

Existing `transport: "http"` configuration is normalized to `streamable-http` automatically. Endpoint paths are otherwise preserved. The only built-in compatibility migration changes the legacy Google Docs URL `https://docsmcp.googleapis.com/mcp` to the working `/mcp/v1` endpoint.

Remote plain HTTP endpoints are rejected unless the host is loopback. Connector secrets are resolved only when the connector starts and are not returned by the dashboard or activity monitor.

### Google Docs

The v2.6.0 runtime migrates the legacy Google Docs `/mcp` URL to the supported versioned endpoint. For the connector currently configured in this project, use:

```text
https://docsmcp.googleapis.com/mcp/v1
```

The compatibility migration is limited to this exact Google Docs host and legacy path. Other MCP endpoint paths are not rewritten.

After configuring the OAuth client and connector, authorize it with:

```powershell
bin\tautline.exe -auth-mcp google_docs
```

The OAuth token is stored under `runtime/v2/oauth/` and is ignored by Git.

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
│   ├── tautline/            # Tautline v2.6.0 source and embedded web assets
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
