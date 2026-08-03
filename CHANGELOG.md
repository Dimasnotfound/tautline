# Changelog

All notable changes to Tautline are documented here.

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
