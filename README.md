<p align="center">
  <img src="assets/tautline.svg" alt="Tautline logo" width="104" />
</p>

<h1 align="center">Tautline</h1>

<p align="center">
  <strong>A context-safe, local-first MCP control plane for ChatGPT.</strong><br />
  Workspace tools, one live activity monitor, external MCP integrations, controlled sub-agents, Lightpanda browsing, and Cloudflare Tunnel in one Go runtime.
</p>

<p align="center">
  <a href="https://github.com/Dimasnotfound/devspace-mcp/actions/workflows/ci.yml"><img alt="CI status" src="https://github.com/Dimasnotfound/devspace-mcp/actions/workflows/ci.yml/badge.svg" /></a>
  <img alt="Tautline version 2.4.0" src="https://img.shields.io/badge/version-2.4.0-38bdf8" />
  <img alt="Go 1.24 or newer" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8" />
  <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/badge/license-MIT-818cf8" /></a>
</p>

## Overview

Tautline lets ChatGPT work with explicitly allowed local project folders without uploading an entire repository into a conversation. It provides bounded file and command tools, secure storage for large command output, a local dashboard, optional 9Router sub-agents, native Lightpanda browser tools, Cloudflare Tunnel support, and a generic MCP client that can republish tools from other MCP servers.

Version 2.4.0 replaces the older per-tool widget system with one live activity monitor. `open_workspace` creates the monitor, while later file, command, browser, skill, agent, and external-MCP actions update the same interface.

## Main capabilities

| Area | Capability |
|---|---|
| Workspace | Open configured roots, search files, read bounded windows, write atomically, edit exact text, execute bounded commands, and review aggregate changes. |
| Context safety | Keep normal responses compact and place oversized command output in secure, redacted local artifacts. |
| Activity monitor | Display recent workspace and global tool activity in one MCP Apps resource. |
| MCP integrations | Connect to local `stdio` servers or remote Streamable HTTP MCP endpoints and republish their tools with unique prefixes. |
| Google Docs | Use Google's official remote Docs MCP endpoint after one-time OAuth authorization. |
| Sub-agents | Delegate controlled tasks through an OpenAI-compatible 9Router endpoint with model and capability allowlists. |
| Browser | Use Lightpanda native browser tools with optional persistent local session state. |
| Dashboard | Configure integrations, agents, browser runtime, tunnel settings, and activity locally. |

## Requirements

- Go 1.24 or newer.
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

When replacing an instance that still runs from the former `chatgpt-mcp` workspace, double-click `SWITCH_TO_TAUTLINE.cmd`. It stops processes tied to the old workspace, starts this build, and verifies that the v2.4.0 health endpoint is available.

## Existing local installation migration

For a private machine migration, preserve only the state still required by the new workspace:

```text
.env
.owner_token.txt
runtime/v2/config/tautline.json
runtime/v2/oauth/
runtime/v2/state/agents.json
runtime/v2/state/dashboard-admin.key
```

Do not migrate generated artifacts, logs, PID files, caches, temporary command output, old binaries, or Lightpanda virtual disks into the repository. These files are recreated when required.

Private files and all runtime state are excluded by `.gitignore`. Never commit them.

## Default endpoints

| Component | Default |
|---|---|
| Dashboard | `http://127.0.0.1:7688/` |
| MCP | `http://127.0.0.1:7688/mcp` |
| Health | `http://127.0.0.1:7688/healthz` |
| 9Router | `http://127.0.0.1:20128/v1` |
| Lightpanda CDP | `http://127.0.0.1:9223` |
| Runtime directory | `runtime/v2/` |
| Activity widget | `ui://tautline/activity-v1.html` |

## Configuration

Copy [`.env.example`](.env.example) to `.env` or use `scripts/setup.ps1`. Important settings include:

| Variable | Purpose |
|---|---|
| `TAUTLINE_ALLOWED_ROOTS` | Comma-separated folders Tautline may access. Keep this list narrow. |
| `TAUTLINE_OWNER_TOKEN` | Secret used during OAuth owner authorization. |
| `TAUTLINE_REQUIRE_AUTH` | Keep `true` whenever the MCP endpoint is exposed. |
| `TAUTLINE_RUNTIME_DIR` | Local runtime configuration and state directory. |
| `TAUTLINE_WIDGETS` | `on` or `off`. Legacy `changes` and `full` values are treated as `on`. |
| `TAUTLINE_PUBLIC_BASE_URL` | Public HTTPS origin used by OAuth metadata. |
| `TAUTLINE_WIDGET_DOMAIN` | Stable HTTPS origin declared for MCP Apps. |
| `TAUTLINE_9ROUTER_ALLOWED_MODELS` | Comma-separated model allowlist for delegated tasks. |
| `TAUTLINE_LIGHTPANDA_PATH` | `auto`, `docker`, `wsl`, or an executable path. |
| `TAUTLINE_TUNNEL_MODE` | `off`, `quick`, or `named`. |

Dashboard changes are saved atomically to `runtime/v2/config/tautline.json`. Environment variables override stored configuration at startup.

## Single activity monitor

Tautline registers one MCP App resource:

```text
ui://tautline/activity-v1.html
```

The monitor shows recent activity and a selected-event inspector. It is updated by workspace operations, commands, secure artifact reads, skill loading, sub-agent actions, Lightpanda tools, and tools republished from external MCP servers.

The bounded in-memory activity buffer is cleared when Tautline stops. Event payloads are redacted, size-limited, and shortened before being exposed to the monitor.

## External MCP integrations

Tautline supports:

- Local child-process integrations through `stdio`.
- Local or remote Streamable HTTP MCP endpoints.
- OAuth-enabled HTTP integrations.
- Environment placeholders such as `${VARIABLE_NAME}` for secret values.

Remote plain HTTP endpoints are rejected unless the host is loopback. Connector secrets are resolved only when the connector starts and are not returned by the dashboard or activity monitor.

### Google Docs

The v2.4.0 runtime supports Google's official Docs MCP endpoint:

```text
https://docsmcp.googleapis.com/mcp/v1
```

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
│   ├── tautline/            # Tautline v2.4.0 source and embedded web assets
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
