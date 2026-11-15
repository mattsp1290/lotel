# Agent Instructions

This repo uses **bn** (Beans) for issue tracking. Run `bn prime` before starting work.
Issues live in the shared hub at `~/.beans/hub`, not in this repository.
Run `bn status` to verify that the current repository resolves to project `lotel`.

## Mission

Use `lotel` as your local OpenTelemetry control plane while coding:

- keep telemetry collection running during development
- query traces/metrics/logs to validate behavior during debugging
- verify observability coverage and signal quality before changes go live

Treat telemetry as a first-class test artifact, not an optional extra.

## What lotel does

`lotel` manages a local OTel Collector subprocess, ingests JSONL signal data into DuckDB, and exposes JSON query commands for agent-friendly checks.

Core command surface:

- `lotel start --wait`
- `lotel stop`
- `lotel status`
- `lotel health`
- `lotel ingest`
- `lotel query traces`
- `lotel query metrics`
- `lotel query logs`
- `lotel query genai calls`
- `lotel query genai usage`
- `lotel query aggregate`
- `lotel prune`

## Standard operating loop

Use this loop by default unless the task explicitly does not involve runtime behavior.

```bash
# 1) Ensure collection and automatic ingestion are up
go build -o lotel ./cmd/lotel
./lotel start --wait
./lotel health
./lotel status

# 2) Run app/tests that emit OTLP telemetry
#    OTLP gRPC: localhost:4317
#    OTLP HTTP: localhost:4318

# 3) Wait for SDK export, Collector batching, and automatic ingestion
# Check ingestion.last_success and ingestion.last_error in status.
./lotel status

# 4) Query and verify expected behavior
./lotel query traces --service <service-name> --since 15m
./lotel query metrics --service <service-name> --since 15m
./lotel query logs --service <service-name> --since 15m
```

## Workflow A: Active development

Use while implementing features and refactors.

1. Start `lotel` early in the session (`start --wait`, then `health`).
2. After each meaningful code change + run, wait for fresh telemetry to appear in queries. Use `ingest` for an immediate catch-up if needed.
3. Confirm expected spans exist for key paths (main request, DB call, external API call).
4. Check metrics for obvious regressions (latency growth, error spikes).
5. For LLM features, confirm `./lotel query genai calls --service <svc> --since 10m` lists the expected model calls with non-null token counts.
6. Keep queries time-bounded with `--since` to avoid stale interpretation.

Suggested checks:

```bash
./lotel query traces --service <service-name> --since 10m --limit 50
./lotel query aggregate --metric <duration-metric> --service <service-name> --since 10m
```

## Workflow B: Debugging and incident reproduction

Use when behavior is wrong, flaky, or unexpectedly slow.

1. Reproduce the issue locally with telemetry enabled.
2. Wait for automatic ingestion after reproduction, or run `ingest` for an immediate catch-up.
3. Inspect traces first for path/call timing and missing downstream spans.
4. Correlate with logs/metrics in the same time window.
5. Repeat after each fix attempt and compare outputs.

For LLM failures, inspect `./lotel query genai calls --errors-only --content --since 30m`
and `./lotel query logs --event-name gen_ai.client.inference.operation.details --since 30m`.

Debug focus examples:

```bash
./lotel query traces --service <service-name> --since 30m --limit 200
./lotel query logs --service <service-name> --since 30m --limit 200
./lotel query aggregate --metric <duration-metric> --service <service-name> --since 30m
```

## Workflow C: Pre-production readiness checks

Use before merging risky changes or promoting to production.

Goal: verify the change is observable, not just functional.

Checklist:

- LLM calls emit `gen_ai.operation.name`, `gen_ai.provider.name`, and `gen_ai.usage.*` attributes
- critical user flows emit traces with expected span structure
- key SLI-like metrics are present and queryable
- error paths emit logs with enough diagnostic context
- no obvious telemetry dropouts in expected windows

Example gate commands:

```bash
./lotel health
./lotel status
./lotel query traces --service <service-name> --since 1h --limit 500
./lotel query metrics --service <service-name> --since 1h --limit 500
./lotel query logs --service <service-name> --since 1h --limit 500
```

## Key paths

- `cmd/lotel/main.go` - CLI entrypoint (Cobra commands)
- `internal/collector/` - subprocess lifecycle (start/stop/status/health)
- `internal/config/` - config resolution and defaults
- `internal/storage/` - DuckDB schema, JSONL ingestion, query, prune
- `internal/storage/genai.go` - GenAI aliases, calls, and usage queries
- `examples/collector/` - opt-in content redaction and hash recipes
- `scripts/genai_integration.py` - optional live OpenCode Go test
- `scripts/verify.py` - end-to-end verification script

## Issue tracking (bn)

```bash
bn ready                           # Find available work
bn show <id>                       # View issue details
bn update <id> --claim              # Claim work
bn close <id> -r "Completed work"    # Complete work with a reason
bn sync                            # Synchronize the shared hub
```

Every mutating `bn` command commits and pushes the hub by default. Keep hub files
out of lotel commits. Use `bn list --closed` to inspect migrated issue history.
See [issue tracking](docs/issue-tracking.md) for setup and migration details.

## Quality gates

Must pass before closing work:

```bash
go test ./...
go build ./...
```

## Landing the plane

When ending a session, do all of the following:

1. Run quality gates: `go test ./... && go build ./...`
2. Update issue status: `bn close <id> -r "Completed in <commit>"`
3. Sync and push:
   ```bash
   git pull --rebase && bn sync && git push
   ```
