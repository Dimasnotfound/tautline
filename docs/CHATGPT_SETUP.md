# Connect Tautline v2.8.0 to ChatGPT

Tautline listens on `127.0.0.1:7688` by default. ChatGPT requires a stable HTTPS origin that forwards to the local MCP endpoint.

## 1. Prepare private configuration

Create `.env` with the setup script:

```powershell
./scripts/setup.ps1 -AllowedRoots "D:\Projects"
```

Keep these settings enabled for any public connection:

```env
TAUTLINE_REQUIRE_AUTH=true
TAUTLINE_WIDGETS=on
TAUTLINE_CODEX_INSTRUCTIONS=on
```

Set the exact public origin without `/mcp`:

```env
TAUTLINE_PUBLIC_BASE_URL=https://your-domain.example
TAUTLINE_WIDGET_DOMAIN=https://your-domain.example
```

Tautline reads optional primary-host guidance from `$CODEX_HOME/config.toml`, falling back to `%USERPROFILE%\.codex\config.toml`. It loads `model_instructions_file` when configured and also reuses the first non-empty global `AGENTS.override.md` or `AGENTS.md`. Invalid explicit configuration falls back to Tautline instructions during normal startup and blocks release preflight.

## 2. Build and start

```powershell
./scripts/build.ps1
./scripts/start.ps1
```

Verify locally:

```text
Dashboard: http://127.0.0.1:7688/
Health:    http://127.0.0.1:7688/healthz
MCP:       http://127.0.0.1:7688/mcp
```

## 3. Configure the HTTPS tunnel

For an existing named Cloudflare Tunnel, configure:

```env
TAUTLINE_CLOUDFLARED_PATH=bin/cloudflared.exe
TAUTLINE_TUNNEL_MODE=named
TAUTLINE_TUNNEL_NAME=your-tunnel-name
TAUTLINE_TUNNEL_PROTOCOL=http2
TAUTLINE_TUNNEL_AUTOSTART=true
```

The public hostname must forward to `http://127.0.0.1:7688`.

## 4. Add the MCP server in ChatGPT

Use the public cache-busting MCP URL for a fresh ChatGPT application:

```text
https://your-domain.example/mcp/v2
```

The original `/mcp` endpoint remains available for existing applications. ChatGPT/OpenAI registrations receive the proven v2.4-compatible public client contract, while trusted loopback callbacks receive persisted dynamic client IDs. Both endpoints advertise `tautline offline_access`, PKCE S256, endpoint-specific Protected Resource Metadata, and the additional Authorization Server/OIDC discovery paths probed by current ChatGPT backends. Do not reuse a failed v2.5.3 or early v2.6 application draft.

Complete the owner authorization flow with the private token stored in `.env` or `.owner_token.txt`. Never paste that token into public documentation, source control, or issue reports.

## 5. Verify the activity monitor

After reconnecting the plugin, send a MyLocal request. At the beginning of every user turn that uses Tautline, the server instructions make ChatGPT call `tautline_activity` exactly once before other Tautline tools. The call renders:

```text
ui://tautline/activity-v5.html
```

Starting the local executable alone cannot open an iframe because widget rendering is initiated by a ChatGPT tool call. Each rendered widget receives a unique prompt `monitor_id`. When the next prompt begins, the previous widget becomes archived and stops automatic polling, so it cannot show activity from later prompts. Use `workspace_lookup` before `open_workspace`; both tools and all later workspace, command, browser, skill, agent, and external-MCP calls remain data-only. Use the widget's **Latest** button to return from an older selected event to live tracking.

## 6. Google Docs integration

Tautline v2.7.0 exposes Google Docs tools directly from the Go runtime and uses the regular Google Docs REST API. Enable the Google Docs API in the selected Google Cloud project and register this OAuth redirect URI:

```text
http://127.0.0.1:8765/oauth/callback
```

Set the OAuth client values in `.env`, then authorize locally:

```powershell
bin\tautline.exe -auth-google-docs
```

Verify real read access without modifying a document:

```powershell
bin\tautline.exe -test-google-docs DOCUMENT_ID
```

An existing `docsmcp.googleapis.com` connector is migrated automatically into the top-level native `google_docs` configuration. The previous OAuth scopes and token path are retained, and the remote preview connector is disabled. The token remains under `runtime/v2/oauth/`, refreshes automatically, and is excluded by `.gitignore`.

## Troubleshooting

- Run `bin\tautline.exe -doctor` first for a read-only summary and concrete corrective actions.
- Confirm `healthz` reports service `Tautline`, version `2.8.0`, the published tool count, 9Router status, and `google_docs.mode` as `native-rest` when Google Docs is enabled.
- Confirm the tunnel forwards to port `7688`.
- Confirm the public base URL and widget domain contain only the HTTPS origin.
- Confirm canonical Protected Resource Metadata returns `/mcp`, versioned metadata returns `/mcp/v2`, and both advertise `tautline offline_access`.
- Confirm unauthenticated requests to `/mcp` and `/mcp/v2` return `401` with endpoint-specific `WWW-Authenticate` metadata URLs.
- Confirm the Authorization Server and OIDC-compatible discovery aliases return the same issuer and registration endpoint.
- When upgrading from a failed v2.5.3 or early v2.6 connection, remove that application and create a fresh one with `/mcp/v2` after the corrected runtime is active.
- Preserve `runtime/v2/state/oauth-clients.json` across normal restarts and migrations, but never commit it.
- Confirm `.env`, owner tokens, OAuth tokens, runtime state, and executables remain untracked.
