# Changelog

All notable changes to DevSpace MCP are documented here.

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
