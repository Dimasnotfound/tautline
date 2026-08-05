# AGENTS.md

## Scope

These instructions apply to the entire repository.

## Project

Tautline is a Go-based, local-first MCP control plane.

- `cmd/tautline/` contains the main server and runtime components.
- `cmd/tautline/web/` contains the embedded dashboard.
- `cmd/lightpanda-shim/` contains the optional browser bridge.
- `scripts/` contains setup, build, start, stop, and switch commands.
- `docs/` contains setup and development guidance.

Keep generated output and machine-local state out of source changes. Read the relevant sections of `README.md`, `docs/CODING_WORKFLOW.md`, `CONTRIBUTING.md`, and `SECURITY.md` before changing behavior.

## Required Ponytail skill

For every non-trivial implementation, bug fix, refactor, review, or architecture task:

1. For a non-trivial turn, call `skills_search` exactly once with the complete resolved request before using workspace tools. That call also creates the prompt activity widget, so do not call `tautline_activity` in the same turn. For a trivial status check or direct workspace request that skips skill matching, call `tautline_activity` once before other Tautline tools.
2. Load `software-development/ponytail` with `skill_view`, even when another skill ranks first.
3. Use Ponytail at `full` intensity unless the user explicitly requests `lite` or `ultra`.
4. Follow its ladder: question the need, prefer the Go standard library, prefer native platform behavior, reuse existing dependencies, and write only the minimum code that works.
5. Prefer deletion, existing code, fewer files, and the smallest correct diff.
6. Do not add speculative abstractions, configuration, dependencies, scaffolding, or compatibility layers.
7. For non-trivial logic, add the smallest useful regression check in the nearest existing test file.

If skill tools are unavailable, apply the same ladder directly.

Ponytail must not remove validation, required error handling, data-loss protection, security controls, accessibility basics, or behavior explicitly requested by the user.

## Workflow

1. Inspect `git status` and preserve unrelated changes.
2. Search for exact symbols before reading large files.
3. Reuse existing patterns and keep edits local to the owning component.
4. Run checks justified by the change:
   - Go: format modified files, then run `go test -count=1 ./...` and `go vet ./...`.
   - Dashboard JavaScript: run `node --check cmd/tautline/web/app.js` and relevant Go tests.
   - Python: syntax-check modified files and run relevant Go tests.
   - Documentation only: run `git diff --check`.
   - Release or cross-platform changes: run `./scripts/build.ps1` on Windows or `./scripts/build.sh` on macOS/Linux.
5. After the final file modification, call `show_changes` exactly once.

Do not claim a check passed when it was skipped or failed.

## Completion

Report changed files, checks run, and any deliberate omission with the condition that would justify adding it later.
