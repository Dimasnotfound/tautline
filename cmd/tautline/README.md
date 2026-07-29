# Tautline executable package

This directory contains the complete Tautline server executable. It remains a single Go package so the source reorganization does not change runtime behavior or internal API boundaries.

## File map

| Area | Files |
|---|---|
| Entrypoint and identity | `main.go`, `version.go` |
| Configuration and runtime | `config.go`, `runtime.go` |
| Workspace and context handling | `workspaces.go`, `context_io.go`, `changes.go` |
| MCP tool registration | `tools.go`, `agent_tools.go`, `workflow.go` |
| Sub-agents and routing | `agents.go`, `router.go` |
| Dashboard | `dashboard.go`, `web/` |
| MCP Apps widgets | `ui_*.go` |
| Authentication and networking | `oauth.go`, `tunnel.go` |
| Browser integration | `lightpanda.go` |
| Hermes skills | `skills.go`, `bridge/` |
| Regression tests | `tools_test.go`, `runtime_test.go` |

## Build

From the repository root:

```bash
go build ./cmd/tautline
```

Use the repository build scripts for the complete formatting, syntax, test, vet, and binary checks.
