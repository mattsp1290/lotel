# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is lotel

lotel (Local OpenTelemetry) is a Rust CLI tool that runs a native OTLP collector for local development. It receives traces, metrics, and logs via gRPC/HTTP, stores raw JSONL, ingests into DuckDB, and provides JSON query commands. The goal is to make telemetry a first-class test artifact during local dev without Docker or distributed backends.

## Build and test commands

```bash
cargo build --workspace          # Build all crates
cargo test --workspace           # Run all tests
cargo fmt --check                # Format check
cargo clippy --workspace --all-targets -- -D warnings  # Lint (zero warnings policy)
```

Run a single test:
```bash
cargo test -p lotel-collector test_name
cargo test -p lotel-storage test_name
cargo test -p lotel-cli test_name
```

CI runs format check, clippy, build, and test in that order (`.github/workflows/rust.yml`).

## Issue tracking

This repo uses **bd** (beads) for issue tracking. Run `bd onboard` once before starting work. Use `bd ready` to find work, `bd show <id>` to view, `bd close <id>` to complete.

## Architecture

Three workspace crates with a clear data pipeline:

```
App (OTLP gRPC :4317 / HTTP :4318)
  → lotel-collector (receives, batches, writes JSONL to ~/.lotel/data/)
  → lotel-cli ingest (reads JSONL, flattens proto records, inserts into DuckDB)
  → lotel-cli query (SQL queries against DuckDB, returns JSON to stdout)
```

**lotel-cli** (`crates/lotel-cli/src/`) — CLI entry point and daemon lifecycle
- `main.rs` — Clap command definitions, routes to handler functions
- `daemon.rs` — Spawns/stops collector as a background process, manages `~/.lotel/collector.state`
- `time.rs` — Parses relative durations ("1h", "7d") and RFC3339 timestamps

**lotel-collector** (`crates/lotel-collector/src/`) — OTLP receiver and pipeline
- `config.rs` — YAML config parsing, embedded default config, path resolution
- `pipeline.rs` — Orchestrates receivers → batch processor → file exporter via tokio channels and CancellationToken
- `model.rs` — Flattens OpenTelemetry proto types into SpanRecord/MetricRecord/LogRecord for storage
- `receiver/grpc.rs` — Tonic gRPC server implementing TraceService, MetricsService, LogsService
- `receiver/http.rs` — Axum HTTP server for `/v1/{traces,metrics,logs}`
- `processor/batch.rs` — Accumulates signals, flushes on timeout or batch size
- `exporter/file.rs` — Writes JSONL files
- `extension/health.rs` — Health check endpoint at :13133

**lotel-storage** (`crates/lotel-storage/src/`) — DuckDB persistence and query
- `db.rs` — Opens DuckDB, runs migrations (creates traces/metrics/logs tables)
- `ingest.rs` — Reads JSONL files, deserializes proto JSON, flattens, inserts into DuckDB
- `query.rs` — Builds parameterized SQL queries, returns typed JSON results
- `prune.rs` — Deletes data older than cutoff, supports dry-run

Integration test at `crates/lotel-collector/tests/integration_test.rs` covers the full roundtrip: config → pipeline → HTTP send → JSONL verify → ingest → query → prune → shutdown.

## Key conventions

- **Async**: tokio runtime, mpsc channels for pipeline data flow, CancellationToken for graceful shutdown
- **Error handling**: `thiserror` for library error enums, `anyhow::Result` with `.context()` in application code
- **Serialization**: `OtlpNano` custom deserializer handles both i64 and string nanosecond timestamps from proto JSON
- **Config resolution**: checks `./lotel-collector.yaml` first, falls back to `~/.lotel/collector-config.yaml`
- **Data directory**: `~/.lotel/data/` for JSONL files and `lotel.db`
- **rustfmt**: edition 2024, max_width 100
- **CLI output**: JSON to stdout, errors to stderr

## Quality gates (must pass before closing work)

```bash
cargo test --workspace && cargo build --workspace
```


### Session Retrospective

After completing work and pushing via `git push`, ask the user if they'd like to do a quick retrospective. Walk through each category below and propose concrete artifacts (not just observations):

- **CLAUDE.md updates** — Did we discover architecture, gotchas, tooling, or patterns not documented here? Propose specific additions.
- **Memory entries** — Any project decisions, user preferences, or non-obvious context worth persisting? Write to `~/.claude/projects/.../memory/` with proper frontmatter.
- **Known issues** — Pre-existing test failures, tech debt, or broken things we discovered but didn't fix. Document so future sessions don't waste time rediscovering them.
- **Permissions & tooling** — Did we repeatedly need permissions we didn't have? Tools we used that should be pre-allowed in `.claude/settings.local.json`?
- **What worked** — Approaches or debugging strategies that were effective. If non-obvious, document so future sessions can reuse them.

Keep it to ~5 minutes. Skip categories with nothing to report. The goal is to make the *next* session start smarter than this one did.