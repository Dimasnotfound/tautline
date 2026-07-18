# Security Policy

DevSpace can read, modify, and execute commands inside explicitly configured local directories. Treat the server as privileged software.

## Safe defaults

- OAuth bearer authentication is enabled unless explicitly disabled.
- Authorization uses the OAuth Authorization Code flow with PKCE S256.
- File access is limited to canonical paths inside `DEVSPACE_ALLOWED_ROOTS`.
- Symlink targets are resolved before the allowed-root check.
- File writes use a temporary file and atomic replacement.
- Shell output and file reads are bounded.
- Commands have a configurable timeout and their process tree is terminated on cancellation.
- Widget CSP declares no external network or resource domains.
- Secrets, runtime files, binaries, and local environment files are ignored by Git.

## Deployment rules

1. Never expose `/mcp` publicly with `DEVSPACE_REQUIRE_AUTH=false`.
2. Use a unique, randomly generated `DEVSPACE_OWNER_TOKEN` for every installation.
3. Keep `DEVSPACE_ALLOWED_ROOTS` as narrow as possible. Do not point it at an entire system drive or home directory without understanding the risk.
4. Protect the machine running DevSpace. OAuth does not protect against malware already running locally.
5. Review every write, edit, and command approval in ChatGPT.
6. Rotate the owner token if terminal logs, `.env`, or an access token may have been exposed.
7. Do not commit `.env`, tunnel credentials, certificates, tokens, PID files, logs, or compiled binaries.

## Reporting a vulnerability

Please open a private GitHub security advisory instead of a public issue. Include the affected version, reproduction steps, impact, and any proposed mitigation. Do not include real tokens, private paths, or user data.

## Supported versions

Security fixes are applied to the latest release line.
