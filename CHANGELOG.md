# Changelog

All notable changes to Tautline are documented here.

## [2.9.0] - 2026-08-05

### Added

- ChatGPT Relay Agents as the default sub-agent backend. `delegate_task` returns a worker prompt for a manually opened ordinary ChatGPT conversation, which claims the task through `claim_agent_task`, reports progress through `update_agent_run`, and submits its result through `complete_agent_task`.
- Random one-time relay join codes, private in-memory worker tokens, constant-time token comparison, timeout and cancellation handling, and activity metadata sanitization.
- Dashboard and diagnostics support for switching between `chatgpt-relay` and the optional legacy `9router` backend.

### Changed

- Sub-agent delegation no longer requires 9Router by default and makes no Codex or OpenAI API model request. Legacy 9Router execution remains available when explicitly selected.
- Router health probes are skipped while ChatGPT relay is active.

### Security

- Relay join codes cannot be reused, worker tokens are never added to activity payloads, and both secrets are cleared when a run completes, fails, times out, or is cancelled.

## [2.8.1] - 2026-08-05

### Fixed

- `skills_search` now starts and renders a fresh prompt monitor before recording its own event, so a skipped standalone `tautline_activity` call cannot route new work into the previous prompt's widget.
- Prompt bootstrap fields are attached to the `skills_search` response metadata, allowing the rendered app to bind immediately to the new `monitor_id`.

### Changed

- `skills_search` is the prompt-boundary tool for non-trivial turns, while `tautline_activity` remains the explicit fallback for trivial status checks and direct workspace turns that skip skill matching.
- Advanced the widget resource to `ui://tautline/activity-v6.html` to invalidate cached v5 tool and widget metadata.

## [2.8.0] - 2026-08-05

### Added

- Managed Git worktree workspaces through `open_workspace` with `mode=worktree` and optional `base_ref`, including detached isolation, dirty-source reporting, bounded creation under `TAUTLINE_WORKTREE_ROOT`, workspace metadata, and restart-safe registry restoration.
- Interactive pipe-backed process sessions through `exec_command` and `write_stdin`, with incremental polling, stdin delivery, stdin closure, Unix Ctrl-C or Windows Ctrl-Break, process-tree termination, workspace ownership checks, bounded redacted output, and shutdown cleanup.

### Changed

- Workspace persistence now records checkout/worktree mode, source checkout, and managed worktree metadata while remaining compatible with the earlier roots-only state file.
- Server workflow guidance now distinguishes reusable checkout workspaces from intentionally new isolated worktrees and routes long-running commands through process sessions instead of the one-shot `bash` tool.

### Security

- Managed worktrees may only originate from allowed repositories and must remain inside the configured Tautline worktree root; restored entries that violate either boundary are ignored.
- Process sessions are scoped to the workspace that created them, cap retained and returned output, redact secret-looking values, and terminate active process trees during Tautline shutdown.

## [2.7.2] - 2026-08-05

### Added

- Read-only `tautline_doctor` MCP tool and `-doctor` CLI mode for checking the active version, configuration, allowed roots, OAuth protection, Google Docs authorization, external MCP connections, published tool count, 9Router, Lightpanda, tunnel status, and concrete corrective actions without returning secret values.
- Bounded `read_many` MCP tool for reading up to ten UTF-8 workspace files in one call while preserving the existing `read` path guards, line windows, cursors, SHA-256 freshness checks, provenance, per-file errors, and aggregate output limits.

### Changed

- `/healthz` now includes the total published MCP tool count and current 9Router status so the CLI doctor can diagnose a running process without privileged dashboard access.
- `-doctor` loads configuration without creating or rewriting the runtime configuration file.

## [2.7.1] - 2026-08-05

### Fixed

- Each activity widget now keeps the `monitor_id` from its own prompt bootstrap and ignores later bootstrap broadcasts for different prompts.
- Archived widgets no longer switch to the newest prompt monitor when ChatGPT publishes updated global tool output, even when the workspace is unchanged.

### Changed

- Advanced the widget resource to `ui://tautline/activity-v5.html` so ChatGPT does not reuse the cached v4 widget behavior.

## [2.7.0] - 2026-08-05

### Added

- Native `gdocs_read_doc` and `gdocs_update_doc` tools implemented directly in Go through `docs.googleapis.com`, without Node.js or the Google Docs MCP Developer Preview endpoint.
- Direct Google OAuth authorization with PKCE through `-auth-google-docs`, automatic access-token refresh, atomic token persistence, and a read-only `-test-google-docs <document-id>` diagnostic.
- Native Google Docs status in `/healthz` and staged-release verification that distinguishes the built-in REST integration from external MCP connectors.

### Changed

- Existing official `docsmcp.googleapis.com` connector configuration migrates automatically to the native Google Docs integration while preserving OAuth scopes, credentials, timeout, and token path.
- The legacy remote Google Docs MCP connector is disabled during migration so the native tools keep the same `gdocs_` names without duplicate registration.

### Security

- Google API responses are size-limited, OAuth refresh tokens remain local under the runtime directory, and token updates continue to use restrictive permissions plus atomic replacement.

## [2.6.0] - 2026-08-04

### Added

- Primary ChatGPT host guidance loader for Codex `model_instructions_file` and the global `AGENTS.override.md` or `AGENTS.md` file under `CODEX_HOME`.
- External MCP transport modes for Streamable HTTP, legacy SSE, and automatic HTTP-to-SSE compatibility fallback.
- Isolated Windows staged-runtime preflight that verifies version, host instructions, enabled MCP connections, and a complete fresh ChatGPT OAuth registration/PKCE/refresh/MCP flow before stopping the active installation.
- `-PreflightOnly` switch mode for validating a release without changing the running Tautline process.

### Changed

- Tautline now merges imported Codex guidance with its authoritative local workspace, tool, and safety instructions through the primary `server.WithInstructions(...)` path only.
- Legacy external MCP `transport: "http"` values are normalized to `streamable-http`; endpoint paths remain unchanged except for the exact legacy Google Docs `/mcp` URL, which migrates to `/mcp/v1`.
- External MCP clients now follow a consistent Start, Initialize, and ListTools lifecycle across stdio, Streamable HTTP, and SSE.
- MCP status now reports the active transport selected at runtime.

### Fixed

- Fresh ChatGPT/OpenAI registrations preserve the proven v2.4 public-client contract (`client_id: chatgpt.com`) while loopback and other trusted development clients continue to use unique, persisted dynamic client IDs.
- OAuth discovery advertises `offline_access`, PKCE S256, resource indicators, and both canonical `/mcp` and cache-busting `/mcp/v2` protected resources.
- Protected Resource and Authorization Server metadata are served at the standard paths plus the path variants probed by current ChatGPT backends; each MCP endpoint returns an endpoint-specific `WWW-Authenticate` challenge.
- Dynamic OAuth client registrations are persisted under the runtime state directory so loopback development connections continue to work after a Tautline restart, while legacy v2.4 tokens remain temporarily compatible.
- Legacy SSE integrations no longer fail with `transport not started yet`.
- Google Docs and other Streamable HTTP integrations retain defensive gzip decoding and their exact configured endpoint.
- The update launcher no longer stops v2.4 or another active runtime before the replacement binary and configured MCP integrations pass an isolated health check.
- Invalid Codex instruction configuration falls back safely at normal startup and fails clearly during release preflight instead of silently activating an incomplete setup.

## [2.5.3] - 2026-08-04

### Added

- Defensive gzip response decoding for Streamable HTTP MCP servers, including responses that omit a usable `Content-Encoding` header.
- Staged Windows update handoff with health verification, automatic rollback, and temporary-file cleanup.

### Changed

- Updated `github.com/mark3labs/mcp-go` from v0.47.1 to v0.57.0 and raised the Go requirement to 1.25.5.
- Preserved configured external MCP endpoint paths instead of rewriting Google Docs `/mcp/v1` URLs to `/mcp`; the forced rewrite returned HTTP 400 and disconnected the connector.
- Extended the Windows build script so the main executable and Lightpanda shim can be built to independent staging paths.

### Fixed

- Google Docs `read_doc` and `update_doc` calls no longer fail when a compressed response reaches the JSON decoder as raw gzip bytes.
- `SWITCH_TO_TAUTLINE.cmd` no longer requires a prebuilt replacement binary and no longer leaves `.next` or `.previous` executables after a successful or rolled-back update.

## [2.5.2] - 2026-08-03

### Added

- One unique prompt-scoped activity monitor for every user turn that uses Tautline.
- A **Latest** button that exits historical inspection, scrolls to the newest event, and resumes automatic tracking.
- Widget-instance state persistence for the selected event and pinned/latest mode.

### Changed

- `tautline_activity` now runs exactly once at the beginning of each Tautline user turn, even when older widgets are present.
- `activity_snapshot` now requires the prompt widget's random `monitor_id` instead of following the globally active workspace.
- Advanced the widget resource to `ui://tautline/activity-v4.html` to prevent cached v3 widgets from using the old global polling contract.

### Fixed

- Older widgets no longer display activity produced by later prompts or other conversations.
- Archived widgets stop automatic polling while retaining access to their own historical event details.
- Following the latest event now keeps the newest timeline entry visible at the top.

## [2.5.1] - 2026-08-03

### Added

- Dedicated no-argument `tautline_activity` render tool for first-use widget activation.
- Active workspace ID persistence so a newly mounted widget restores the latest project automatically.
- Empty-state bootstrap support when no workspace has been registered yet.

### Changed

- Moved widget output metadata from `open_workspace` to `tautline_activity`; every operational tool remains data-only.
- Made `activity_snapshot.workspace_id` optional for app polling and active-workspace recovery.
- Updated the monitor to follow the active workspace without remounting its iframe.
- Advanced the widget resource to `ui://tautline/activity-v3.html` to avoid stale client caches.

### Fixed

- The monitor no longer depends on `open_workspace` being called after each Tautline restart or ChatGPT reconnection.

### Notes

- Starting the local executable cannot render ChatGPT UI by itself. The first user-triggered MyLocal interaction calls `tautline_activity` and mounts the widget.

## [2.5.0] - 2026-08-03

### Added

- `workspace_lookup` for reusing an already-open project without mounting another widget card.
- A small local workspace registry at `runtime/v2/state/workspaces.json` so existing widget sessions can recover their workspace after restart.
- Semantic event themes for read, write, edit, create, delete, changes, command, skill, agent, and failure activity.
- Bounded detail caching for previously inspected activity entries.

### Changed

- Replaced full widget rerenders with a static shell, incremental timeline updates, and event delegation.
- Added independent scrolling for the timeline and preview inspector while preserving their positions during polling.
- Removed the Changes summary block, Latest and Refresh controls, and the separate metadata-chip row.
- Added adaptive polling that slows from 1.4 seconds to at most 5 seconds while activity is unchanged.
- Advanced the single widget resource to `ui://tautline/activity-v2.html` to avoid stale v2.4 client caches.
- Repeated `open_workspace` calls for an already-open folder are rejected before another monitor can mount.

### Security

- Kept `open_workspace` as the only tool with widget output metadata; `workspace_lookup`, `activity_snapshot`, and every other tool remain data-only.
- Retained bounded, redacted, in-memory activity payloads with no new external assets or network origins.

## [2.4.0] - 2026-08-02

### Added

- One live workspace activity monitor at `ui://tautline/activity-v1.html`.
- Generic external MCP integrations for local `stdio` and Streamable HTTP servers.
- OAuth support for external MCP connectors, including the official Google Docs MCP endpoint.
- Native Lightpanda MCP tools, persistent session settings, and private-network controls.
- A Windows build step that embeds the current Tautline application icon.

### Changed

- Removed the previous per-tool widget resources and compatibility cards.
- Moved the active v2.4.0 source into the clean `cmd/tautline` repository layout.
- Updated configuration examples to use `TAUTLINE_*` names while retaining required Hermes bridge compatibility variables.
- Simplified local migration to preserve only configuration, OAuth data, agent state, and dashboard credentials.

### Security

- External connector secrets are resolved only at startup and are not returned by the dashboard or activity monitor.
- Activity payloads are redacted, bounded, and held only in process memory.
- Runtime credentials, executables, generated resources, logs, caches, and artifacts remain excluded from Git.

## [2.1.0] - 2026-07-29

### Added

- Global dashboard switch for enabling or pausing new sub-agent delegation.
- Strict multi-model allowlist through `TAUTLINE_9ROUTER_ALLOWED_MODELS`.
- Dashboard model cards and a default-model selector limited to allowed models.
- Local SVG application icon for the dashboard and browser tab.
- Regression tests for allowlist normalization, global delegation control, forbidden requested models, forbidden returned models, and embedded dashboard assets.

### Changed

- Sub-agent tools now expose the global enabled state, default model, and complete model allowlist.
- Delegation rejects a requested model outside the allowlist.
- A completed 9Router response is rejected when the actual returned model is outside the allowlist.
- Dashboard copy and controls are shorter, cleaner, and more responsive.
- Default sub-agent capacity is two slots and the default timeout is 900 seconds.
- Application source moved from the repository root into `cmd/tautline`, with embedded dashboard and Hermes bridge assets grouped beside the executable package.

### Security

- New delegation can be disabled globally without exposing router credentials in dashboard state.
- Both caller-selected and router-returned models are checked against the configured allowlist.

## [2.0.0] - 2026-07-28

### Added

- Tautline v2 identity and a local dashboard on port `7688`.
- Generic asynchronous sub-agent capacity through an OpenAI-compatible 9Router endpoint.
- Explicit image capability gates and in-memory image request handling.
- Lightpanda-only browsing with executable, PATH, Docker, and WSL resolution.
- Secure large-output artifacts with bounded previews and follow-up reads.
- Read-only Hermes skill discovery and loading.
- Cloudflare quick tunnel, named tunnel, and DNS route controls.
- Atomic configuration at `runtime/v2/config/tautline.json`.

### Changed

- The main widget moved to `ui://tautline/tool-card-v2.html` with legacy aliases for migration.
- The main binary is built from the repository root.
- New configuration uses `TAUTLINE_*` environment variables.

## [1.4.0] - 2026-07-18

### Added

- Codex-style workspace sessions with reusable `workspace_id` values and relative paths.
- `DEVSPACE_WIDGETS=changes` as the quiet default UI mode.
- Aggregate `show_changes` tool with one review checkpoint per editing turn.
- Clean `changes-review` widget with file totals inline and the complete diff in fullscreen.
- Server instructions that guide the model through inspect, edit, verify, and review.

### Changed

- `read`, `write`, `edit`, and `bash` no longer attach custom widgets in the default mode.
- The workspace card is smaller and appears only when a workspace opens.
- `full` mode preserves the v1.3 per-tool widget behavior; `off` disables custom UI.

## [1.3.0] - 2026-07-18

### Added

- Public, secret-safe repository structure.
- Compact model-visible workspace payload with full widget hydration in hidden tool-result metadata.
- Reproducible workspace payload and token benchmark.
- Symlink-aware canonical path enforcement.
- Configurable shell, runtime directory, cloudflared path, and named tunnel.
- Secure PowerShell setup script that generates a random owner token.
- Linux and Windows CI plus Gitleaks scanning.
- README hero, architecture, and benchmark visualizations.

### Changed

- Default allowed root is now the process working directory instead of a machine-specific path.
- Automatic tunnel startup is disabled by default.
- Configured owner tokens are hidden from startup output by default.
- Tool descriptors include human-readable titles and MCP Apps visibility metadata.

### Security

- Removed all private domains, machine-specific roots, tokens, binaries, logs, runtime state, and historical archives from the public release.
- Added explicit deployment guidance in `SECURITY.md`.

## [1.2.0] - 2026-07-18

- Added dedicated workspace, file, diff, and command widgets.
- Added fullscreen support, light/dark mode, CSP metadata, and structured output schemas.

## [1.1.0] - 2026-07-18

- Added OAuth Authorization Code + PKCE, bearer validation, atomic writes, bounded commands, proper diffs, and non-streaming Streamable HTTP responses.
