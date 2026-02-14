# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

## Project Overview

**lotel** is a CLI tool for local OpenTelemetry. It manages an OTel Collector subprocess and provides telemetry querying via DuckDB.

## Key Paths

- `cmd/lotel/main.go` — CLI entrypoint (Cobra commands)
- `internal/collector/` — Subprocess lifecycle (start/stop/status/health)
- `internal/config/` — Config resolution and defaults
- `internal/storage/` — DuckDB schema, JSONL ingestion, query, prune
- `scripts/verify.py` — End-to-end verification script

## Quality Gates

```bash
go test ./...     # All tests must pass
go build ./...    # Must compile
```

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd close <id>         # Complete work
```

## Landing the Plane

When ending a session, you MUST:

1. Run quality gates (`go test ./... && go build ./...`)
2. Update issue status with `bd close`
3. Push to remote:
   ```bash
   git pull --rebase && bd sync && git push
   ```
