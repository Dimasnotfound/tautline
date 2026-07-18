# Contributing

Thank you for helping improve DevSpace MCP.

## Development

```bash
go fmt ./...
go test ./...
go vet ./...
go build ./cmd/devspace
```

Run the server locally without a tunnel:

```bash
go run ./cmd/devspace -start -tunnel=false
```

Copy `.env.example` to `.env` first. Never use production secrets in tests or issue reports.

## Pull requests

- Keep changes focused and explain the user-facing impact.
- Add or update tests for security-sensitive behavior.
- Keep each widget below 20 KiB and avoid external scripts, fonts, images, or network calls.
- Use a new versioned `ui://` URI for substantial widget changes so ChatGPT does not reuse a stale resource.
- Preserve bounded reads, bounded output, cancellation, path canonicalization, OAuth, and CSP protections.
- Do not add generated binaries or local configuration.

## Commit style

Use clear imperative messages, for example:

```text
Harden symlink path validation
Add workspace payload benchmark
Improve command result widget
```
