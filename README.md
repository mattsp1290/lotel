# lotel — Local OpenTelemetry

A CLI tool for local OpenTelemetry telemetry collection, querying, and management. Runs a native OTLP collector and stores telemetry in DuckDB for fast querying.

## Scope

`lotel` is designed for **one developer machine / one host** while building and debugging software locally. It is intentionally not a distributed telemetry backend and is not meant to replace production observability stacks. Its goal is to provide local development with a coding agent a full loop that lets you replicate the observability experience you'd have of a deployed service. Your agent has probably always had access to logs. But adding in traces lets them start debugging things like call length or answer questions like "Are we actually making API calls here?".

## Quick Start

```bash
# Build lotel
cargo build --release

# Start the collector
./target/release/lotel-cli start --wait

# Send telemetry to localhost:4317 (gRPC) or localhost:4318 (HTTP)
# Then ingest and query:
./target/release/lotel-cli ingest
./target/release/lotel-cli query traces --service my-app
./target/release/lotel-cli query metrics --service my-app
./target/release/lotel-cli query logs --service my-app
```

## Commands

| Command | Description |
|---------|-------------|
| `lotel-cli start [--wait]` | Start the OTel Collector |
| `lotel-cli stop` | Stop the collector |
| `lotel-cli status` | Show collector status (JSON) |
| `lotel-cli health` | Check collector health (exit 0/1) |
| `lotel-cli ingest` | Ingest JSONL files into DuckDB |
| `lotel-cli query traces` | Query traces (JSON output) |
| `lotel-cli query metrics` | Query metrics (JSON output) |
| `lotel-cli query logs` | Query logs (JSON output) |
| `lotel-cli query aggregate` | Compute avg/min/max for a metric |
| `lotel-cli prune` | Delete telemetry older than threshold |

## Query Options

All query commands support:

```
--service     Filter by service.name
--since       Start time (RFC3339 or relative: "1h", "24h", "7d")
--until       End time (RFC3339)
--limit       Max results
```

### Examples

```bash
# Traces from the last hour
lotel-cli query traces --service my-app --since 1h

# Metric aggregation over a time window
lotel-cli query aggregate --metric http_request_duration --service my-app --since 24h

# Prune data older than 7 days (dry run first)
lotel-cli prune --older-than 7d --dry-run
lotel-cli prune --older-than 7d
```

## Output Contract

All query commands output JSON to stdout. Exit codes:
- `0`: success
- `1`: error or unhealthy status

This makes lotel suitable for scripted and agent-driven workflows.

### AI Coding Agent Workflows

`lotel` works well with AI coding agents (like Cursor agents, Claude Code, or similar tools) because commands are local, deterministic, and JSON-first. An agent can:

- start and health-check telemetry collection before running app/tests
- ingest newly produced telemetry artifacts after test runs
- query traces/metrics/logs with filters (`--service`, `--since`) to validate behavior
- use non-zero exit codes to fail fast in automation loops

## Architecture

```
Application → OTLP (gRPC :4317 / HTTP :4318)
    → lotel-collector (native process)
        → Batch processor
            → ~/.lotel/data/{traces,metrics,logs}/*.jsonl
                → lotel-cli ingest → DuckDB (~/.lotel/data/lotel.db)
                    → lotel-cli query → JSON output
```

## Library Usage

The collector is also available as a Rust library:

```toml
[dependencies]
lotel-collector = { path = "crates/lotel-collector" }
lotel-storage = { path = "crates/lotel-storage" }
```

```rust
let collector = lotel_collector::Collector::with_defaults()?;
let handle = collector.start()?;
handle.wait_healthy(Duration::from_secs(30)).await?;
// ... application runs, sends OTLP data ...
handle.shutdown().await;
```

## Data Storage

- **Raw**: JSONL files written by the collector to `~/.lotel/data/{traces,metrics,logs}/`
- **Indexed**: DuckDB database at `~/.lotel/data/lotel.db` (populated by `lotel-cli ingest`)
- **State**: PID and config at `~/.lotel/collector.state`
- **Config**: Default config at `~/.lotel/collector-config.yaml` (auto-generated)

## Configuration

lotel looks for collector config in this order:
1. `./lotel-collector.yaml` (project-local)
2. `~/.lotel/collector-config.yaml` (auto-generated default)

The default config provides OTLP receivers (gRPC + HTTP), batch processing, and file exporters for all three signals.

## Requirements

- Rust stable toolchain (1.80+)
- No Docker required — collector runs as a native process

## License

MIT
