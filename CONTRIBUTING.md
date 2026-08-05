# Contributing

Thank you for helping improve Tautline.

## Development setup

1. Fork or clone the repository.
2. Install Go 1.25.5 or newer, Python 3, and Node.js.
3. Copy `.env.example` to `.env` only when runtime testing is required.
4. Keep `TAUTLINE_ALLOWED_ROOTS` limited to disposable or intended test projects.
5. Do not commit `.env`, tokens, runtime state, logs, artifacts, or binaries.

## Quality gate

Run the complete repository gate before opening a pull request:

```bash
./scripts/build.sh
```

Windows PowerShell:

```powershell
./scripts/build.ps1
```

The gate verifies Go formatting, the Python bridge, dashboard and Laju Relay Bridge JavaScript, extension manifest scope, unit tests, `go vet`, the main binary, and the Lightpanda shim.

## Pull requests

- Keep changes focused and explain the user-visible behavior.
- Add or update regression tests for behavior changes.
- Update `README.md`, `.env.example`, `CHANGELOG.md`, and security guidance when configuration or public behavior changes.
- Preserve context-safe bounded output and allowed-root enforcement.
- Do not weaken OAuth, dashboard session, CSRF, model allowlist, image capability, or path validation protections.
- Use repository-relative examples rather than machine-specific paths or private domains.

## Commit style

Use concise imperative commit subjects, for example:

```text
Add strict router model allowlist
Fix dashboard agent toggle state
Document Tautline v2.1 setup
```
