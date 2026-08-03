# Changelog

All notable changes to Tautline are documented here.

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
