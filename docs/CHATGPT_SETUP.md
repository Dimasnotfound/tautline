# Connect DevSpace to ChatGPT

This guide assumes DevSpace already runs locally on `127.0.0.1:7676` and is reachable through a stable public HTTPS origin.

## 1. Configure DevSpace

Create `.env` with at least:

```dotenv
DEVSPACE_ALLOWED_ROOTS=/path/to/your/projects
DEVSPACE_OWNER_TOKEN=<long-random-secret>
DEVSPACE_REQUIRE_AUTH=true
DEVSPACE_PUBLIC_BASE_URL=https://mcp.example.com
```

On Windows, `scripts/setup.ps1` generates the token and `.env` safely.

## 2. Expose the server

Use a reverse proxy or tunnel you control. It must forward the public HTTPS origin to:

```text
http://127.0.0.1:7676
```

The MCP URL presented to ChatGPT is:

```text
https://mcp.example.com/mcp
```

Do not expose an unauthenticated endpoint.

## 3. Enable ChatGPT Developer Mode

In ChatGPT settings:

1. Enable **Developer mode**.
2. Enable **Enforce CSP in developer mode**.
3. Create a developer-mode app using the MCP URL.
4. Complete the OAuth approval page with your owner token.
5. Refresh the app after changing tools, schemas, or `ui://` resource URIs.

## 4. Verify the UI

Select DevSpace in the conversation and ask:

```text
Use DevSpace to open /path/to/allowed/project.
```

With the recommended `DEVSPACE_WIDGETS=changes` mode, a successful Apps SDK connection renders one compact workspace card. Normal read, write, edit, and command calls remain native tool results. After editing a disposable test file, call `show_changes` once to verify the aggregate review card.

## 5. Troubleshooting

### Tools work, but no widget appears

- Confirm the app was created in Developer Mode, not only as a generic connector.
- Refresh the app metadata.
- Start a new conversation if ChatGPT still uses old descriptors.
- Verify each tool includes `_meta.ui.resourceUri` and an `outputSchema`.
- Verify each resource uses `text/html;profile=mcp-app`.

### OAuth fails

- Check `DEVSPACE_PUBLIC_BASE_URL` exactly matches the public HTTPS origin.
- Confirm the callback host is a ChatGPT/OpenAI host or localhost during local testing.
- Generate a new owner token if the configured token is unknown.

### Cloudflare stream cancellation messages

DevSpace disables standalone GET streaming and uses stateless Streamable HTTP responses. A cancellation log can still appear when a client intentionally ends a request; confirm `/healthz` remains available before treating it as an outage.
