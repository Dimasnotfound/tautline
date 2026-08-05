# Security Policy

Tautline can read and modify files, execute commands, delegate work to external model providers through 9Router, and expose a local MCP endpoint through a tunnel. Treat it as privileged development software.

## Safe defaults

- OAuth bearer authentication is enabled unless explicitly disabled.
- Authorization uses OAuth Authorization Code with PKCE S256. ChatGPT/OpenAI callbacks use the proven v2.4 public client contract, while trusted loopback callbacks use exact, persisted dynamic registrations.
- New access and refresh tokens are bound to the client and selected `/mcp` or `/mcp/v2` resource; legacy unbound tokens remain accepted only for migration compatibility.
- File access is limited to canonical paths inside `TAUTLINE_ALLOWED_ROOTS`.
- Symlink targets are resolved before the allowed-root check.
- File writes use temporary files and atomic replacement.
- File reads, multi-file reads, command output, and artifact previews are bounded.
- `tautline_doctor` and `-doctor` report only readiness, paths, counts, and masked status; they never return owner tokens, OAuth tokens, router keys, connector headers, or connector environment values.
- One-shot commands have bounded timeouts and process-tree termination. Interactive process sessions are workspace-scoped, output-bounded, secret-redacted, explicitly interruptible or terminable, and killed during Tautline shutdown.
- Managed worktrees can originate only from allowed Git repositories and are accepted or restored only inside `TAUTLINE_WORKTREE_ROOT`; they are an isolation boundary for workflow, not a security sandbox.
- The dashboard binds to `127.0.0.1` and requires a local bootstrap-derived session.
- Dashboard mutations require CSRF validation.
- 9Router API keys are not included in dashboard state.
- New sub-agent delegation can be disabled globally.
- Requested and returned 9Router models must be present in the configured allowlist.
- Runtime state, local environment files, secrets, logs, and binaries are ignored by Git.
- Native Google Docs OAuth tokens remain under the configured runtime directory, are refreshed only through Google's OAuth endpoint, and are sent only to the Google Docs REST API.

## Deployment rules

1. Never expose `/mcp` publicly with `TAUTLINE_REQUIRE_AUTH=false`.
2. Use a unique random `TAUTLINE_OWNER_TOKEN` for every installation.
3. Keep `TAUTLINE_ALLOWED_ROOTS` as narrow as possible. Do not use an entire system drive or home directory unless the risk is fully understood.
4. Keep `TAUTLINE_9ROUTER_ALLOWED_MODELS` limited to models approved for delegated work.
5. Keep `TAUTLINE_AGENT_ENABLED=false` when delegation is not required.
6. Keep `TAUTLINE_WORKTREE_ROOT` inside a private, Tautline-owned directory and do not treat managed worktrees as a shell sandbox.
7. Protect the machine running Tautline. OAuth does not protect against malware already running locally.
8. Review write, edit, command, and delegation approvals in ChatGPT.
9. Rotate the owner token or router key if terminal output, `.env`, runtime files, or an access token may have been exposed.
10. Do not commit `.env`, tunnel credentials, certificates, tokens, PID files, logs, artifacts, runtime configuration, or compiled binaries.
11. Use a stable HTTPS origin and ensure `TAUTLINE_PUBLIC_BASE_URL` and `TAUTLINE_WIDGET_DOMAIN` match the intended public origin exactly.

## Sub-agent and image data

Delegated tasks are sent to the configured 9Router endpoint and may be processed by external model providers selected by that router. Review the provider's privacy and retention policy before sending sensitive material.

Image payloads are accepted only when the selected slot allows images, the request explicitly requires images, and model capability has been verified. Tautline does not intentionally persist in-memory image payloads to run state, configuration, logs, or runtime files, but this does not control retention by an external provider.

## Reporting a vulnerability

Open a private GitHub security advisory instead of a public issue. Include the affected version, reproduction steps, impact, and any proposed mitigation. Do not include real tokens, private paths, credentials, or user data.

## Supported versions

Security fixes are applied to the latest release line. The currently documented public release is Tautline v2.8.0.
