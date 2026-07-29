# Connect Tautline to ChatGPT

This guide uses Tautline v2.1.0 on `127.0.0.1:7688` and a stable public HTTPS origin that forwards requests to the local service.

## 1. Create local configuration

Windows PowerShell:

```powershell
./scripts/setup.ps1 `
  -AllowedRoots "D:\Projects" `
  -PublicBaseUrl "https://mcp.example.com" `
  -WidgetDomain "https://mcp.example.com" `
  -AllowedModels "auto"
```

macOS or Linux:

```bash
cp .env.example .env
```

Review `.env` and set at least:

```env
TAUTLINE_ALLOWED_ROOTS=/path/to/your/projects
TAUTLINE_OWNER_TOKEN=<long-random-secret>
TAUTLINE_REQUIRE_AUTH=true
TAUTLINE_PUBLIC_BASE_URL=https://mcp.example.com
TAUTLINE_WIDGET_DOMAIN=https://mcp.example.com
TAUTLINE_WIDGETS=changes
```

Keep allowed roots narrow and never commit `.env`.

## 2. Configure optional sub-agents

Sub-agents require an OpenAI-compatible 9Router endpoint. Configure one default model and an explicit allowlist:

```env
TAUTLINE_9ROUTER_BASE_URL=http://127.0.0.1:20128/v1
TAUTLINE_9ROUTER_API_KEY=
TAUTLINE_9ROUTER_MODEL=auto
TAUTLINE_9ROUTER_ALLOWED_MODELS=auto
TAUTLINE_AGENT_ENABLED=true
TAUTLINE_AGENT_CAPACITY=2
TAUTLINE_AGENT_TIMEOUT_SECONDS=900
```

Tautline rejects both requested and returned models outside the allowlist. Set `TAUTLINE_AGENT_ENABLED=false` when delegation is not needed.

## 3. Build and start

Windows PowerShell:

```powershell
./scripts/build.ps1
./scripts/start.ps1
```

macOS or Linux:

```bash
./scripts/build.sh
./scripts/start.sh
```

Verify the local service:

```text
http://127.0.0.1:7688/healthz
```

The dashboard opens locally and can be used to manage model access, sub-agent capacity, Lightpanda, and tunnel settings.

## 4. Expose the MCP endpoint

Use a quick or named Cloudflare Tunnel from the local dashboard, or configure another trusted reverse proxy. The public MCP endpoint must resolve to:

```text
https://mcp.example.com/mcp
```

`TAUTLINE_PUBLIC_BASE_URL` and `TAUTLINE_WIDGET_DOMAIN` must contain the public origin only, without `/mcp`.

The dashboard itself remains local and returns `404` without a valid local admin session.

## 5. Add the MCP connection

Create a custom MCP connection in ChatGPT using the public `/mcp` URL. Complete the OAuth flow with the owner token stored in `.env`.

After changing the public origin, OAuth metadata, tool descriptors, or widget resources, refresh the MCP connection metadata before testing again.

## 6. Verify the workflow

Ask ChatGPT to open a project inside an allowed root:

```text
Use Tautline to open /path/to/allowed/project and explain the repository structure.
```

With `TAUTLINE_WIDGETS=changes`, the expected coding flow is:

1. `open_workspace` returns one compact workspace card and a reusable `workspace_id`.
2. Search, read, command, write, and edit calls stay compact.
3. `show_changes` renders one aggregate review after the final modification.
4. Repository-specific tests and builds run before completion is reported.

## Troubleshooting

- Confirm `/healthz` succeeds locally.
- Confirm the public tunnel forwards to port `7688`.
- Confirm the public origin exactly matches `TAUTLINE_PUBLIC_BASE_URL` and `TAUTLINE_WIDGET_DOMAIN`.
- Confirm the requested project is inside `TAUTLINE_ALLOWED_ROOTS`.
- Confirm `TAUTLINE_REQUIRE_AUTH=true` for a public deployment.
- Refresh the MCP connection after changing OAuth or widget metadata.
- Check the dashboard for 9Router, model allowlist, sub-agent, Lightpanda, and tunnel status.
- For rejected delegation, confirm global delegation is enabled and the requested model appears in the allowlist.

Tautline uses stateless Streamable HTTP responses and disables standalone GET streaming. A client may intentionally cancel a completed request; verify `/healthz` before treating that log entry as an outage.
