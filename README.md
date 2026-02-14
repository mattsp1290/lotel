# lotel — Local OpenTelemetry

A CLI tool for local OpenTelemetry telemetry collection, querying, and management. Runs the OTel Collector as a local subprocess and stores telemetry in DuckDB for fast querying.

## Quick Start

```bash
# Install the collector binary
# See: https://github.com/open-telemetry/opentelemetry-collector-releases

# Build lotel
go build -o lotel ./cmd/lotel

# Start the collector
./lotel start --wait

# Send telemetry to localhost:4317 (gRPC) or localhost:4318 (HTTP)
# Then ingest and query:
./lotel ingest
./lotel query traces --service my-app
./lotel query metrics --service my-app
./lotel query logs --service my-app
```

## Commands

| Command | Description |
|---------|-------------|
| `lotel start [--wait]` | Start the OTel Collector subprocess |
| `lotel stop` | Stop the collector |
| `lotel status` | Show collector status (JSON) |
| `lotel health` | Check collector health (exit 0/1) |
| `lotel ingest` | Ingest JSONL files into DuckDB |
| `lotel query traces` | Query traces (JSON output) |
| `lotel query metrics` | Query metrics (JSON output) |
| `lotel query logs` | Query logs (JSON output) |
| `lotel query aggregate` | Compute avg/min/max for a metric |
| `lotel prune` | Delete telemetry older than threshold |

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
lotel query traces --service my-app --since 1h

# Metric aggregation over a time window
lotel query aggregate --metric http_request_duration --service my-app --since 24h

# Prune data older than 7 days (dry run first)
lotel prune --older-than 7d --dry-run
lotel prune --older-than 7d
```

## Output Contract

All query commands output JSON to stdout. Exit codes:
- `0`: success
- `1`: error or unhealthy status

This makes lotel suitable for scripted and agent-driven workflows.

## Architecture

```
Application → OTLP (gRPC :4317 / HTTP :4318)
    → OTel Collector (subprocess)
        → ~/.lotel/data/{traces,metrics,logs}/*.jsonl
            → lotel ingest → DuckDB (~/.lotel/data/lotel.db)
                → lotel query → JSON output
```

## Data Storage

- **Raw**: JSONL files written by the collector to `~/.lotel/data/{traces,metrics,logs}/`
- **Indexed**: DuckDB database at `~/.lotel/data/lotel.db` (populated by `lotel ingest`)
- **State**: Collector PID and config at `~/.lotel/collector.state`
- **Config**: Default config at `~/.lotel/collector-config.yaml` (auto-generated)

## Configuration

lotel looks for collector config in this order:
1. `./lotel-collector.yaml` (project-local)
2. `~/.lotel/collector-config.yaml` (auto-generated default)

The default config provides OTLP receivers (gRPC + HTTP), batch processing, and file exporters for all three signals.

## Verification

```bash
# Run the end-to-end verification script
pip install requests
python3 scripts/verify.py
```

## Requirements

- Go 1.24+
- `otelcol-contrib` binary on PATH
- CGO enabled (for DuckDB)

### Installing otelcol-contrib

Download the latest release from [opentelemetry-collector-releases](https://github.com/open-telemetry/opentelemetry-collector-releases/releases) for your platform, extract, and place on PATH.

**Linux x86_64:**

```bash
curl -LO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.143.1/otelcol-contrib_0.143.1_linux_amd64.tar.gz
tar xzf otelcol-contrib_0.143.1_linux_amd64.tar.gz
sudo mv otelcol-contrib /usr/local/bin/
```

**Linux ARM64:**

```bash
curl -LO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.143.1/otelcol-contrib_0.143.1_linux_arm64.tar.gz
tar xzf otelcol-contrib_0.143.1_linux_arm64.tar.gz
sudo mv otelcol-contrib /usr/local/bin/
```

**macOS ARM (Apple Silicon):**

```bash
curl -LO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.143.1/otelcol-contrib_0.143.1_darwin_arm64.tar.gz
tar xzf otelcol-contrib_0.143.1_darwin_arm64.tar.gz
sudo mv otelcol-contrib /usr/local/bin/
```

Verify the installation:

```bash
otelcol-contrib --version
```

## License

MIT
