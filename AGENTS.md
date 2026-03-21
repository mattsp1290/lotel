# Agent Instructions

This repo uses **bd** (beads) for issue tracking. Run `bd onboard` once before starting work.

## Mission

Use `lotel` as your local OpenTelemetry control plane while coding:

- keep telemetry collection running during development
- query traces/metrics/logs to validate behavior during debugging
- verify observability coverage and signal quality before changes go live

Treat telemetry as a first-class test artifact, not an optional extra.

## What lotel does

`lotel` manages a local OTel Collector process, ingests JSONL signal data into DuckDB, and exposes JSON query commands for agent-friendly checks.

Core command surface:

- `lotel-cli start --wait`
- `lotel-cli stop`
- `lotel-cli status`
- `lotel-cli health`
- `lotel-cli ingest`
- `lotel-cli query traces`
- `lotel-cli query metrics`
- `lotel-cli query logs`
- `lotel-cli query aggregate`
- `lotel-cli prune`

## Standard operating loop

Use this loop by default unless the task explicitly does not involve runtime behavior.

```bash
# 1) Build and ensure collector is up
cargo build --release
./target/release/lotel-cli start --wait
./target/release/lotel-cli health

# 2) Run app/tests that emit OTLP telemetry
#    OTLP gRPC: localhost:4317
#    OTLP HTTP: localhost:4318

# 3) Ingest fresh telemetry
./target/release/lotel-cli ingest

# 4) Query and verify expected behavior
./target/release/lotel-cli query traces --service <service-name> --since 15m
./target/release/lotel-cli query metrics --service <service-name> --since 15m
./target/release/lotel-cli query logs --service <service-name> --since 15m
```

## Workflow A: Active development

Use while implementing features and refactors.

1. Start `lotel-cli` early in the session (`start --wait`, then `health`).
2. After each meaningful code change + run, execute `ingest`.
3. Confirm expected spans exist for key paths (main request, DB call, external API call).
4. Check metrics for obvious regressions (latency growth, error spikes).
5. Keep queries time-bounded with `--since` to avoid stale interpretation.

Suggested checks:

```bash
./target/release/lotel-cli query traces --service <service-name> --since 10m --limit 50
./target/release/lotel-cli query aggregate --metric <duration-metric> --service <service-name> --since 10m
```

## Workflow B: Debugging and incident reproduction

Use when behavior is wrong, flaky, or unexpectedly slow.

1. Reproduce the issue locally with telemetry enabled.
2. `ingest` immediately after reproduction.
3. Inspect traces first for path/call timing and missing downstream spans.
4. Correlate with logs/metrics in the same time window.
5. Repeat after each fix attempt and compare outputs.

## Workflow C: Pre-production readiness checks

Use before merging risky changes or promoting to production.

Goal: verify the change is observable, not just functional.

Checklist:

- critical user flows emit traces with expected span structure
- key SLI-like metrics are present and queryable
- error paths emit logs with enough diagnostic context
- no obvious telemetry dropouts in expected windows

## Key paths

- `crates/lotel-cli/src/main.rs` - CLI entrypoint (clap commands)
- `crates/lotel-cli/src/daemon.rs` - process lifecycle (start/stop/status/health)
- `crates/lotel-collector/src/` - OTLP receivers, batch processor, file exporter, pipeline
- `crates/lotel-collector/src/config.rs` - config resolution and defaults
- `crates/lotel-storage/src/` - DuckDB schema, JSONL ingestion, query, prune
- `PARITY.md` - Rust rewrite parity tracker

## Issue tracking (bd)

```bash
bd ready       # Find available work
bd show <id>   # View issue details
bd close <id>  # Complete work
```

## Quality gates

Must pass before closing work:

```bash
cargo test --workspace
cargo build --workspace
```

## Landing the plane

When ending a session, do all of the following:

1. Run quality gates: `cargo test --workspace && cargo build --workspace`
2. Update issue status: `bd close <id>`
3. Sync and push:
   ```bash
   git pull --rebase && bd sync && git push
   ```
