# lotel — Local OpenTelemetry

A CLI tool for local OpenTelemetry telemetry collection, querying, and management. Runs the OTel Collector as a Docker container and stores telemetry in DuckDB for fast querying.

## Scope

`lotel` is designed for **one developer machine / one host** while building and debugging software locally. It is intentionally not a distributed telemetry backend and is not meant to replace production observability stacks. Its goal is to provide local development with a coding agent a full loop that lets you replicate the observability experience you'd have of a deployed service. Your agent has probably always had access to logs. But adding in traces lets them start debugging things like call length or answer questions like "Are we actually making API calls here?".

## Quick Start

```bash
# Ensure Docker is installed and running
# See: https://docs.docker.com/get-docker/

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
| `lotel start [--wait]` | Start the OTel Collector container |
| `lotel stop` | Stop the collector |
| `lotel status` | Show collector status (JSON) |
| `lotel health` | Check collector health (exit 0/1) |
| `lotel ingest` | Incrementally ingest new JSONL telemetry (JSON report) |
| `lotel query traces` | Query traces (JSON output) |
| `lotel query metrics` | Query metrics (JSON output) |
| `lotel query logs` | Query logs (JSON output) |
| `lotel query genai calls` | Query model, agent, and tool calls |
| `lotel query genai usage` | Group token usage, errors, and latency |
| `lotel query aggregate` | Compute avg/min/max for a metric |
| `lotel prune` | Delete telemetry older than threshold |

## Query Options

All query commands support `--service`, `--since`, and `--until`. Generic queries and `genai calls` also accept `--limit`:

```
--service     Filter by service.name
--since       Start time (RFC3339 or relative: "1h", "24h", "7d")
--until       End time (RFC3339)
--limit       Max results
```

Additional filters:

| Flag | Commands |
| --- | --- |
| `--name` | `traces` (span name), `metrics` (metric name) |
| `--trace-id` | `traces`, `logs`, `genai calls`, `genai usage` |
| `--event-name` | `logs` |
| `--attr key=value` | `traces`, `metrics`, `logs`; repeat to require all matches |
| `--model`, `--provider`, `--operation`, `--conversation-id`, `--errors-only` | Both GenAI commands |
| `--content` | `genai calls` |
| `--group-by` | `genai usage`; comma-separated `provider,model,operation,service,conversation` |

Attribute filters compare the compact JSON string form. An integer 12 matches
`--attr key=12`. A stored double 1.0 matches `key=1`, not `key=1.0`.
A double 1e21 matches `key=1e21`. Null and missing values never match.
Structured values match their compact JSON text.

Generic queries order oldest first. `genai calls` orders newest first.
Usage defaults to grouping by provider, model, and operation. It reports token
totals, calls with missing usage, errors, and nearest-rank latency percentiles.
For cumulative histograms, read the latest `query metrics --name <name>` point.
`query aggregate` aggregates per-point sums without correcting cumulative temporality.

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

### AI Coding Agent Workflows

`lotel` works well with AI coding agents (like Cursor agents, Claude Code, or similar tools) because commands are local, deterministic, and JSON-first. An agent can:

- start and health-check telemetry collection before running app/tests
- ingest newly produced telemetry artifacts after test runs
- query traces/metrics/logs with filters (`--service`, `--since`) to validate behavior
- use non-zero exit codes to fail fast in automation loops

## Architecture

```
Application → OTLP (gRPC :4317 / HTTP :4318)
    → OTel Collector (Docker container)
        → ~/.lotel/data/{traces,metrics,logs}/*.jsonl
            → lotel ingest → DuckDB (~/.lotel/data/lotel.db)
                → lotel query → JSON output
```

## Data Storage

- **Raw**: JSONL files written by the collector to `~/.lotel/data/{traces,metrics,logs}/`
- **Indexed**: DuckDB database at `~/.lotel/data/lotel.db` (populated by `lotel ingest`)
- **State**: Container ID and config at `~/.lotel/collector.state`
- **Config**: Default config at `~/.lotel/collector-config.yaml` (auto-generated)

Schema v2 preserves typed attributes, resource and scope metadata, span events
and links, structured log bodies, event names, and histogram buckets.
`ingest_state` tracks a byte offset and file fingerprint per JSONL file. Repeated
ingest reads only complete new lines. Malformed lines and skipped records appear
in the ingest report.

On upgrade from an older database schema, lotel rebuilds the index. Run
`lotel ingest` to reload raw history. Previously pruned rows reappear after a
rebuild because prune leaves the raw JSONL files intact.

## Configuration

lotel looks for collector config in this order:
1. `./lotel-collector.yaml` (project-local)
2. `~/.lotel/collector-config.yaml` (auto-generated default)

The collector image is pinned to `otel/opentelemetry-collector-contrib:0.165.0`.
The default config provides OTLP receivers, GenAI normalization, spanmetrics,
batch processing, and file exporters for all three signals.

The managed file starts with `# lotel-managed-config v2`. Lotel replaces older
managed versions. It backs up a file without a managed header to
`collector-config.yaml.bak.<timestamp>` before replacing it. Put custom config
in `./lotel-collector.yaml`; lotel leaves that file untouched.

`lotel start` restarts a running collector when its image or the contents of the
same config path change. If this directory resolves a different config path,
it warns instead. Run `lotel stop` first to switch paths.

File exporters use `append: true`, so raw telemetry survives restarts and grows
until you delete it. Custom configs must also set `append: true`.
File-exporter rotation is unsupported. Spanmetrics uses delta temporality to
avoid appending the same idle series on every flush.

<a id="genai"></a>
## GenAI / LLM observability

The `gen_ai_normalizer` processor maps OpenInference and OpenLLMetry attributes
to OTel GenAI attributes before export. Native OTel attributes take precedence.
The `spanmetrics` connector emits call counts and duration metrics for all spans,
with operation, provider, request model, response model, and error dimensions.

Install the official Python OpenAI instrumentation and the HTTP exporters:

```bash
pip install openai opentelemetry-distro opentelemetry-exporter-otlp-proto-http opentelemetry-instrumentation-genai-openai
export OTEL_SERVICE_NAME=my-llm-app
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_LOGS_EXPORTER=otlp
export OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=span_and_event
opentelemetry-instrument python my_app.py
```

After your app makes calls, ingest and query:

```bash
./lotel ingest
./lotel query genai calls --service my-llm-app --since 10m
./lotel query genai calls --service my-llm-app --since 10m --content
./lotel query genai usage --service my-llm-app --group-by provider,model
./lotel query logs --event-name gen_ai.client.inference.operation.details --since 10m
```

A call includes these fields, among others:

```json
{"provider":"openai","operation":"chat","request_model":"example-model","input_tokens":5,"output_tokens":3,"duration_ms":120}
```

Content is omitted by default. `--content` decodes JSON-string message attributes
and falls back to correlated GenAI event attributes or structured event bodies.
Missing token usage remains null on calls and contributes zero to usage totals.

To remove or hash content, copy
[genai-redact-content.yaml](examples/collector/genai-redact-content.yaml) or
[genai-hash-content.yaml](examples/collector/genai-hash-content.yaml) to
`./lotel-collector.yaml`, then run:

```bash
./lotel stop
./lotel start --wait
```

These recipes process span attributes, span events, log attributes, and structured
GenAI event bodies after normalization. They cover native, OpenInference, and
OpenLLMetry content keys. They preserve token counts.

GenAI conventions have Development status. Lotel stores attributes verbatim and
resolves current and legacy aliases listed in
[genai.go](internal/storage/genai.go). It does not estimate monetary cost.

## Verification

Docker must be running. The deterministic test needs no LLM credentials:

```bash
go test ./...
go build ./...
go vet ./...
go build -o lotel ./cmd/lotel
pip install requests
LOTEL_BIN=./lotel python3 scripts/verify.py
LOTEL_DOCKER_TESTS=1 go test ./internal/config -run 'TestExampleConfigsValidate|TestRedactionRecipesBehavior' -v
```

### Optional live LLM test

Install `uv`, then run the real OpenCode Go test:

```bash
zsh -c 'source ~/.profile && LOTEL_BIN=./lotel uv run scripts/genai_integration.py'
```

Without `OPENCODE_GO_API_KEY`, the script prints `SKIP` and exits 0.
Set `LOTEL_IT_REQUIRE=1` to fail instead. The key stays out of child-process
environments. The test scans local telemetry and collector logs for leaks.

| Variable | Default |
| --- | --- |
| `LOTEL_IT_OPENCODE_MODEL` | `glm-5.3-flash` |
| `LOTEL_IT_OPENCODE_BASE_URL` | `https://opencode.ai/inference/go/openai/v1` |
| `LOTEL_BIN` | `lotel` |
| `OTLP_HTTP_ENDPOINT` | `http://localhost:4318` |
| `LOTEL_IT_REQUIRE` | unset |

The PEP 723 script has a committed dependency lock. Without `uv`, create
`.venv-it`, install the dependencies listed in the script header, and run
`.venv-it/bin/python scripts/genai_integration.py`.

## Issue tracking

This project uses `bn` (Beans), with issues stored in the shared hub at
`~/.beans/hub`. Run `bn prime` for the agent workflow or `bn ready` for available
work. See [issue tracking setup and migration](docs/issue-tracking.md).

## Requirements

- Go 1.24+
- Docker
- CGO enabled (for DuckDB)

## License

MIT
