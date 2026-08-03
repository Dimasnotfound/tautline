# Connect Tautline v2.5.2 to ChatGPT

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
```

Set the exact public origin without `/mcp`:

```env
TAUTLINE_PUBLIC_BASE_URL=https://your-domain.example
TAUTLINE_WIDGET_DOMAIN=https://your-domain.example
```

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

Use the public MCP URL:

```text
https://your-domain.example/mcp
```

Complete the owner authorization flow with the private token stored in `.env` or `.owner_token.txt`. Never paste that token into public documentation, source control, or issue reports.

## 5. Verify the activity monitor

After reconnecting the plugin, send a MyLocal request. At the beginning of every user turn that uses Tautline, the server instructions make ChatGPT call `tautline_activity` exactly once before other Tautline tools. The call renders:

```text
ui://tautline/activity-v4.html
```

Starting the local executable alone cannot open an iframe because widget rendering is initiated by a ChatGPT tool call. Each rendered widget receives a unique prompt `monitor_id`. When the next prompt begins, the previous widget becomes archived and stops automatic polling, so it cannot show activity from later prompts. Use `workspace_lookup` before `open_workspace`; both tools and all later workspace, command, browser, skill, agent, and external-MCP calls remain data-only. Use the widget's **Latest** button to return from an older selected event to live tracking.

## 6. Google Docs integration

Tautline v2.5.2 can connect to Google's official Docs MCP endpoint. Register this OAuth redirect URI in the Google Cloud OAuth client:

```text
http://127.0.0.1:8765/oauth/callback
```

After the connector is configured, authorize it locally:

```powershell
bin\tautline.exe -auth-mcp google_docs
```

The resulting token is stored under `runtime/v2/oauth/` and is excluded by `.gitignore`.

## Troubleshooting

- Confirm `healthz` reports service `Tautline` and version `2.5.2`.
- Confirm the tunnel forwards to port `7688`.
- Confirm the public base URL and widget domain contain only the HTTPS origin.
- Confirm `.env`, owner tokens, OAuth tokens, runtime state, and executables remain untracked.
