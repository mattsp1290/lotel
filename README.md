# lotel

Collect and query OpenTelemetry on your machine. Lotel runs official Go
Collector components as a native process or Docker container and indexes their
JSONL files in DuckDB. Queries return JSON for coding agents and scripts.

`lotel` gives a coding agent local traces, metrics, and logs to check an
application while it runs. The default Collector writes telemetry to your
machine. You do not need a hosted telemetry account, an API key, or a paid
telemetry backend.

It is for one developer machine. Use your production observability system for
shared or deployed environments. Model calls made by your application or coding
agent can still cost money. If an agent reads query output, its model provider
may receive that output under the agent's own data policy.

## Install and upgrade

The release installer becomes available after the first release is published.
Until then, [build from source](#build-from-source).

To install a published binary on Linux or macOS, on x86_64 or arm64:

```bash
curl -fsSL https://github.com/mattsp1290/lotel/releases/latest/download/install.sh | sh
```

The installer verifies the release manifest, SHA-256 checksum, and executable
version before replacing `~/.local/bin/lotel`. It needs `curl`, `awk`, `sed`,
and one of `sha256sum`, `shasum`, or `openssl`. It does not use `sudo` or change your shell
profile. If the install directory is not on your `PATH`, add it:

```bash
export PATH="$HOME/.local/bin:$PATH"
lotel --version
```

Set `LOTEL_INSTALL_DIR` to choose another directory or
`LOTEL_INSTALL_VERSION=vX.Y.Z` to pin a release. Set these variables for `sh`
when you pipe the installer:

```bash
curl -fsSL https://github.com/mattsp1290/lotel/releases/latest/download/install.sh | \
  LOTEL_INSTALL_DIR="$HOME/bin" LOTEL_INSTALL_VERSION=vX.Y.Z sh
```

Update an installed binary:

```bash
lotel upgrade --check
lotel upgrade --check --json
lotel upgrade
lotel health
```

To pin or roll back, run `lotel upgrade --version vX.Y.Z`. Replace `vX.Y.Z`
with a published release tag. Upgrades verify the download before replacing
the executable in place.

Before replacement, `upgrade` warns on stderr that OpenTelemetry collection
will be unavailable. Agents must retain stderr even when they request `--json`.
It stops a running Collector, replaces the executable, then uses the new
executable to restart the Collector with its recorded backend and configuration
and wait for health. An upgrade can therefore apply new Collector versions and defaults.
Telemetry emitted during that gap can be lost unless your application buffers
or retries it. An initially stopped Collector stays stopped. `--check` and upgrades that need no replacement
do not interrupt collection. If the recorded backend cannot report the Collector
state, the upgrade aborts before replacement and preserves the recorded state.

If an upgrade fails after the stop, it attempts to restart collection and reports
any recovery failure. Use the recovery command printed by `upgrade`, including
its recorded `--config` path and native backend flag when present, then run
`lotel health`. Resume telemetry checks only after the Collector is healthy.
Upgrading the executable preserves the local telemetry files. The Collector may
refresh its managed configuration when it starts.

Source builds require `--force` before a binary upgrade can replace them.
Normally, rebuild and reinstall those from source instead. Use `lotel upgrade`
for installed releases so it can manage the collection interruption.
See [the release contract](docs/release.md) for supported systems, mirrors,
upgrade safeguards, and release publishing.

## Quick start

Install and start Docker for the default backend, or use `lotel start --backend native --wait`
to run the official Go Collector components without Docker.

```bash
lotel start --wait
lotel health
```

Configure your application's OpenTelemetry SDK to send OTLP to
`localhost:4317` for gRPC or `http://localhost:4318` for HTTP. After the app
emits telemetry, query the service name that your app uses. Lotel indexes new
telemetry automatically while the Collector runs:

```bash
lotel query traces --service my-app --since 10m
lotel query metrics --service my-app --since 10m
lotel query logs --service my-app --since 10m
lotel query profiles --service my-app --since 10m
lotel query otlp --signal metrics --service my-app --since 10m
```

An empty JSON array means no matching records were found. It is not proof that
the application worked. Check the service name, SDK exporters, sampling, and
export interval. Wait for the SDK, Collector, and automatic ingestion to flush.
Use `lotel ingest` when you need an immediate catch-up.

## Verify an application with a coding agent

Suppose your app has a checkout endpoint that reads a database and calls a
payment API. Ask your agent to verify the behavior using telemetry, then repeat
the same request after each fix. A local fake payment service keeps the example
independent of paid APIs.

First, instrument the application with its language's OpenTelemetry SDK and
OTLP exporters. Add spans around the checkout, database query, and payment
request. Export a request-duration metric and logs with trace IDs. Lotel collects
what the application emits. It does not add application instrumentation.

For an SDK that supports the standard environment variables, run the app with:

```bash
export OTEL_SERVICE_NAME=checkout-dev
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_TRACES_EXPORTER=otlp
export OTEL_METRICS_EXPORTER=otlp
export OTEL_LOGS_EXPORTER=otlp
```

Use these settings in the shell that starts your application or test process.
They configure an installed SDK and exporters. They do not install them.
Remove any vendor exporters or signal-specific endpoint overrides that still
send telemetry elsewhere. See the [OTLP exporter settings](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/)
for SDK support and per-signal configuration.

Give your agent a concrete task such as:

> Start lotel and check its health. Run the checkout success and failed-payment
> tests with service.name=checkout-dev against the local fake payment service.
> Wait for telemetry to flush and appear in queries. Query only this test window.
> Confirm that the checkout span has database and payment child spans, that the
> duration metric has fresh points, and that a failed payment has an error span
> and a useful log with the same trace ID. Treat missing telemetry as a failed
> check. If a check fails, fix the application or its instrumentation, rerun the
> same cases, and compare the new telemetry. Report the commands, trace IDs,
> observed values, and any gaps. Keep the Collector running.

The agent can use these commands after the test run:

```bash
lotel query traces --service checkout-dev --since 10m --limit 100
lotel query metrics --service checkout-dev --since 10m --limit 100
lotel query logs --service checkout-dev --since 10m --limit 100
```

For example, the agent can check that each child span's `parent_span_id` points
to the checkout span's `span_id`. It can then correlate an error with
`lotel query logs --trace-id <trace-id> --since 10m`. Replace `<trace-id>` with
an actual value from the trace query.

Capture a start time before the tests and an end time after the SDK and
Collector flush and the expected metric points appear. Use those times with
`--since` and `--until` when you compare runs. Both accept RFC3339 timestamps.
Use a unique service name per run if the same app also runs in another terminal.
A ten-minute window is convenient for exploration but can mix results from before and after a fix. Successful
queries exit 0 even when empty, so the agent must inspect the records and
compare them against the expected spans, metrics, and logs.

The feedback loop is **run, inspect, fix, rerun**. Local query results
let the agent check whether a code change altered the real execution path.
They can also expose missing instrumentation before the change reaches
production. Review sensitive fields before giving query output to a remote
agent. The [content redaction examples](#genai) cover LLM message content.

## Build from source

Building requires Make, Go 1.26 or later, CGO enabled, and a C/C++ toolchain
for DuckDB. Docker is required for the default backend. Native mode runs the
bundled Collector without Docker.

```bash
git clone https://github.com/mattsp1290/lotel.git
cd lotel
make install
export PATH="$HOME/.local/bin:$PATH"
lotel --version
```

## Commands

| Command | Description |
|---------|-------------|
| `lotel --version` | Show the executable version |
| `lotel commands` | List registered commands and flags as JSON |
| `lotel upgrade [--check] [--json] [--version vX.Y.Z]` | Check for or install a release |
| `lotel start [--wait] [--config <path>]` | Start the Docker Collector and automatic ingestion |
| `lotel start --backend native [--wait]` | Start the bundled Collector without Docker, with automatic ingestion |
| `lotel stop` | Stop the collector |
| `lotel status` | Show collector status (JSON) |
| `lotel health` | Check collector health (exit 0/1) |
| `lotel ingest` | Incrementally ingest new JSONL telemetry (JSON report) |
| `lotel ingest --full` | Atomically rebuild the index from retained JSONL |
| `lotel assert signal` | Fail if a service has no indexed signal in a time window |
| `lotel query traces` | Query traces (JSON output) |
| `lotel query span-stats` | Summarize observed spans by service, name, and kind |
| `lotel query recent traces` / `lotel query recent logs` | Return a bounded newest-first snapshot |
| `lotel query log-stats` | Summarize observed logs by service and severity |
| `lotel query services` | List observed services and their signals |
| `lotel query operations` | List observed span names and kinds |
| `lotel query dependencies` | Summarize observed client-to-server service relationships |
| `lotel query trace <trace-id>` | Query a trace and its correlated logs (JSON output) |
| `lotel query metrics` | Query metrics (JSON output) |
| `lotel query logs` | Query logs (JSON output) |
| `lotel query profiles` | Query complete OTLP profile resource envelopes |
| `lotel query otlp --signal <signal>` | Query complete OTLP envelopes for traces, metrics, logs, or profiles |
| `lotel query genai calls` | Query model, agent, and tool calls |
| `lotel query genai usage` | Group token usage, errors, and latency |
| `lotel query aggregate` | Compute avg/min/max for a metric |
| `lotel prune` | Delete telemetry older than threshold |

`lotel commands` returns a versioned catalog of registered visible commands and
their visible flags. Each entry includes its path, usage text, summary, whether
it can run, and its flags. Flag defaults are strings as shown by the CLI, and
`inherited` identifies flags inherited from a parent command. The catalog
excludes Cobra's automatically generated help and completion commands and flags.
The set of commands and their defaults can change between lotel versions.

The catalog does not describe all required arguments, validation rules, result
schemas, or exit codes. Use the command's help and documentation for those.

## Assert signal evidence

Use `lotel assert signal --signal traces --service my-app --since 10m` to
check for an indexed record. Select `traces`, `metrics`, or `logs` with
`--signal`. The `--since` value is a positive duration or an RFC3339 timestamp.
`--until` accepts an RFC3339 timestamp and defaults to now. Both bounds are
inclusive. The command rejects future end times.

The command returns JSON with the resolved window, `passed`, and, when a
record matches, `latest_timestamp`. It exits 0 for a match and 1 for no match
or an error. A missing match means no projected record is indexed in the
window. It does not prove the application or Collector failed. Allow time for
export and ingestion, and use `lotel status` and `lotel health` for collection
status. Historical assertions do not require a running Collector.

## Query options

`lotel query recent traces --since 15m --limit 100` and `lotel query recent logs
--since 15m --limit 100` return `signal`, resolved `since` and `until`,
`records`, and `truncated`. Both require a bounded start and a limit from 1 to
1000. They support the corresponding trace or log filters. Results order newest
first, with deterministic ordering for equal timestamps. Identical records
remain separate. Time bounds apply to projected microsecond timestamps.

Each result describes one database query. Ingestion, pruning, or rebuilding can
change a later result. This command does not provide a continuation cursor.
The limit bounds returned records, not the work needed to search the database.

Query commands accept different filters. Most support `--service`, `--since`, and `--until`. Generic queries and `genai calls` also accept `--limit`. The single-trace query has no time or service filters, and dependency discovery accepts only `--since` and `--until`:

```
--service     Filter by service.name
--since       Start time (RFC3339 or relative: "1h", "24h", "7d")
--until       End time (RFC3339)
--limit       Max results
```

Service discovery lists services observed in projected traces, metrics, and logs.
It does not include profile-only services or establish whether a service is running.
Operation discovery lists distinct projected span names and kinds by service. Both
commands filter the observation window before grouping and apply the limit after
sorting. Missing service names appear as `unknown`; empty names remain empty.

`lotel query dependencies --since 15m` reports observed cross-service edges
from a CLIENT span to its direct SERVER child. The child's projected start time
selects the window at microsecond precision. The parent and uniqueness checks
use all retained spans, including spans outside the window. The start is required;
the inclusive end defaults to now and cannot be in the future. Edges count unique
child span identities. Each endpoint contains `service.name` and
`service.namespace`. An absent namespace and an empty namespace are equivalent.
Duplicate trace/span identities are excluded rather than
guessed. The `exclusions` counts categorize each excluded SERVER record once, in
the order shown in the JSON. Duplicate child records count individually there.
Missing or invalid projected IDs, missing parents, ambiguous identities, invalid projected
service identity types, unknown services,
non-client parents, and same-service pairs are excluded. An `unknown` service
can be either a real name or the ingestion fallback, so both are excluded. SDK
fallback names `unknown_service` and `unknown_service:<executable>` are also
excluded, even when used as a literal service name.
The report excludes identity attributes that the projection identifies as
non-string values, including explicit JSON null. It also excludes a span when
the projected `service.name` differs from its stored service name. Duplicate
attributes and original types that collapse to the same projected string cannot
always be detected. A missing namespace remains equivalent to an empty string.
Some OTLP values, including bytes and special floating-point values, project as
strings, so their original types cannot always be identified. Legacy spans
without projected resource attributes use their stored service name.
Sampling, missing instrumentation, retention, and ingestion delays can hide
edges. Messaging spans and span links are not represented. This report is not a
complete topology or a request-volume estimate.

Additional filters:

| Flag | Commands |
| --- | --- |
| `--name` | `traces` (span name), `metrics` (metric name) |
| `--trace-id` | `traces`, `logs`, `genai calls`, `genai usage` |
| `--errors-only`, `--min-duration` | `traces` |
| `--min-severity`, `--body-contains` | `logs`, `log-stats` |
| `--event-name` | `logs` |
| `--attr key=value` | `traces`, `metrics`, `logs`; repeat to require all matches |
| `--model`, `--provider`, `--operation`, `--conversation-id`, `--errors-only` | Both GenAI commands |
| `--content` | `genai calls` |
| `--group-by` | `genai usage`; comma-separated `provider,model,operation,service,conversation` |

Attribute filters compare the compact JSON string form. An integer 12 matches
`--attr key=12`. A stored double 1.0 matches `key=1`, not `key=1.0`.
A double 1e21 matches `key=1e21`. Null and missing values never match.
Structured values match their compact JSON text.

For traces, `--errors-only` matches the OpenTelemetry span status ERROR. It does
not infer errors from HTTP status or exceptions. `--min-duration` accepts a
nonnegative Go duration such as `100ms` and includes spans at the threshold.
It compares the projected nanosecond duration. Spans with missing timestamps
have a projected duration of zero.

For logs, `--min-severity` accepts `trace`, `debug`, `info`, `warn`, `error`,
or `fatal`. It compares the OpenTelemetry severity number and excludes
unspecified and out-of-range values. `--body-contains` performs a literal,
case-insensitive substring match on projected body text using DuckDB lowercase
conversion. This is not full Unicode case folding. Structured bodies appear as
JSON text, so keys and JSON syntax can match. This is not a typed field search.
The substring must not be empty.

Generic queries order oldest first. `genai calls` orders newest first.
Usage defaults to grouping by provider, model, and operation. It reports token
totals, calls with missing usage, errors, and nearest-rank latency percentiles.
For cumulative histograms, read the latest `query metrics --name <name>` point.
`query aggregate` aggregates per-point sums without correcting cumulative temporality.

`lotel query span-stats --since 15m` groups projected spans by service, name,
and span kind. The start is required and can be a positive duration or an RFC3339
timestamp. The end defaults to the time of the query; `--until` accepts an
RFC3339 timestamp no later than that time. Both bounds are inclusive and match
the projected span start time, which is stored at microsecond precision. For
example, a span starting at `12:00:00.0000002` is stored at `12:00:00.000000`
and does not match a window starting at `12:00:00.0000002`. `--service` filters
before grouping, and `--limit` limits the sorted groups after aggregation.

Each group reports the number of spans, the number with OpenTelemetry ERROR
status, and their error rate over all spans in the group, including spans with
unset status. Duration percentiles use nearest-rank observed nanosecond values.
The report excludes negative durations and spans with missing start or end
timestamps from the percentile samples. `duration_count` and
`duration_excluded` show how many matched spans were included and excluded.
Percentiles are null when no durations qualify. The report counts projected
records, including duplicates. Sampling and missing telemetry mean the counts
are local observations, not traffic estimates.

`lotel query log-stats --since 15m` groups projected logs by service and
OpenTelemetry severity band. The required start accepts a positive duration or
an RFC3339 timestamp. The inclusive end defaults to the query time, and an
explicit `--until` cannot be in the future. Bounds match projected log
timestamps at microsecond precision. Log ingestion uses the observed timestamp
when the event timestamp is absent and skips records with neither timestamp.

Groups are sorted by service and severity: `unspecified`, `trace`, `debug`,
`info`, `warn`, `error`, `fatal`, then `invalid`. Severity zero is unspecified;
null and out-of-range numbers are invalid. Severity text does not set the band.
The severity and body filters apply before grouping, and `--limit` applies to
the sorted groups. Counts include duplicate projected records. Sampling and
missing telemetry mean the counts are local observations, not traffic estimates.

`lotel query trace <trace-id>` returns one object with `trace_id`, `spans`, and
`logs` arrays for all services and times. The ID must be 32 hexadecimal
characters and nonzero. Parent and log relationships use the existing span IDs.
The output retains duplicate spans and logs with missing or unknown span IDs.
Empty arrays mean no matching projected records were found. As with other
convenience queries, the projections can omit fields or records. Use `query otlp`
for complete retained OTLP envelopes.

Add `--relationships` to resolve span parents and log span IDs within the
returned bundle. The command sorts spans and logs by timestamp and then by
their complete projected JSON before assigning zero-based array indices.
Identical records remain separate and their indices are interchangeable.
Indices can change when the stored records change.

The `relationships.span_parents` and `relationships.log_spans` arrays align
with the returned spans and logs. Each entry has a `state`. A `linked` entry
contains one index in `span_indices`; an `ambiguous` entry contains every
matching index. `missing` means no matching span was found. `invalid` means
the referenced ID is malformed or zero. An empty parent is `root`; an empty
log span ID is `unscoped`. `cycle_span_indices` lists spans that form a cycle
through uniquely resolved parents. Relationships follow recorded IDs and do
not prove causality or a complete trace.

### Complete telemetry data

`query otlp` retains the complete JSON exported by the Collector. Each result
contains `signal`, `service_name`, and `payload`. The payload contains one
resource group with its scopes, records, schemas, and original top-level fields.
Profile dictionaries remain intact so sample references stay valid.

```bash
lotel query otlp --signal traces --service my-app --since 15m
lotel query otlp --signal metrics --service my-app --since 15m
lotel query otlp --signal logs --service my-app --since 15m
lotel query profiles --service my-app --since 15m
```

Use these commands for exact nanosecond timestamps, typed attributes, large
integer metric values, unsigned counts, special floating-point values,
exemplars, flags, dropped counts, and complete resource and scope metadata.
The existing traces, metrics, logs, and GenAI queries remain convenience
projections. Their numeric and timestamp columns can lose precision, and they
do not expose every OTLP field.

Time filters select resource envelopes whose timestamp ranges overlap the
requested window. They return the entire envelope, including records outside
that window. `--limit` counts resource envelopes. Undated envelopes appear in
unbounded queries and are excluded when a time bound is supplied. Service
filters match the resource's `service.name`. These commands do not accept the
record filters such as `--name`, `--trace-id`, or `--attr`.

Retention deletes a complete envelope only when its latest timestamp is older
than the cutoff. This keeps newer records in mixed-age envelopes. Undated
envelopes require `prune --all`. A dictionary can contain unused entries from
other resources because lotel preserves it without rewriting its indices.

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

## Output contract

All query commands output JSON to stdout. Exit codes:
- `0`: success
- `1`: error or unhealthy status

This makes lotel suitable for scripted and agent-driven workflows.
`ingest` and `prune` finish database changes before writing their reports. If
report output fails, those changes can still have completed. Check the database
state before retrying.

## Architecture

```
Application → OTLP (gRPC :4317 / HTTP :4318)
    → OTel Collector (native process or Docker container)
        → ~/.lotel/data/{traces,metrics,logs,profiles}/*.jsonl
            → manual or automatic ingestion → DuckDB (~/.lotel/data/lotel.db)
                → lotel query → JSON output
```

## Data storage

- **Raw**: JSONL files written by the collector to `~/.lotel/data/{traces,metrics,logs,profiles}/`
- **Indexed**: DuckDB database at `~/.lotel/data/lotel.db`, updated automatically while lotel runs
- **State**: Backend, container or process identity, and config at `~/.lotel/collector.state`
- **Config**: Default config at `~/.lotel/collector-config.yaml` (auto-generated)

Schema v3 adds complete OTLP resource envelopes alongside the existing query
projections. `ingest_state` tracks one byte offset and file fingerprint per
JSONL file. Payloads, projections, and progress commit atomically. Repeated
ingestion reads only complete new lines.
Malformed lines and skipped projection records appear in the ingest report.
`otlp_envelopes` counts preserved resource envelopes separately from projected
records. A skipped projection does not imply that its OTLP payload was lost.
`projection_skipped_lines` counts envelopes that the convenience decoder could
not interpret. Prune reports use `otlp_envelopes_deleted` for complete envelopes.

Upgrading from schema v2 to v3 preserves indexed rows and existing ingestion
progress. Complete OTLP retention starts at that progress point, so previously ingested history
remains available through the existing queries. It does not gain complete
OTLP payloads retroactively. Run `lotel ingest --full` to backfill complete
envelopes from retained raw history without duplicating projected rows.
Databases older than v2 still rebuild their index. Run `lotel ingest` to reload
that raw history. Pruned rows can reappear after
a rebuild because prune leaves the raw JSONL files intact.

## Configuration

Without `start --config <path>`, lotel looks for Collector config in this order:
1. `./lotel-collector.yaml` (project-local)
2. `~/.lotel/collector-config.yaml` (auto-generated default)

`start --config <path>` selects an explicit configuration file. Upgrades
preserve the running Collector's recorded config path, even when the command
runs from another directory.

The collector image is pinned to `otel/opentelemetry-collector-contrib:0.165.0`.
The default config provides OTLP receivers, GenAI normalization, spanmetrics,
batch processing, and file exporters for traces, metrics, logs, and profiles.
Profiles bypass the trace-only GenAI normalizer and use the Collector's
`service.profilesSupport` feature gate. Profile protocol support is experimental
and senders must match the schema in the pinned Collector release.
The pinned release accepts HTTP profiles at `/v1development/profiles` on port
4318. The other signals use `/v1/traces`, `/v1/metrics`, and `/v1/logs`.

The managed file starts with `# lotel-managed-config v3`. Lotel replaces older
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

### Run without Docker

The native backend bundles official OpenTelemetry Go Collector v0.165.0
components. It includes the OTLP receiver, batch and GenAI normalizer processors,
transform processor, spanmetrics connector, file exporter, and health extension.
The existing content redaction and hash recipes work with this backend.
Use Docker for custom configurations that need other contrib components.

```bash
go build -o lotel ./cmd/lotel
./lotel start --backend native --wait
./lotel status
./lotel query traces --service my-app --since 10m
./lotel stop
```

Stop an existing collector before changing backends. Docker remains the default.
Both backends use the same raw files and database. Native mode translates file
exporter paths under `/data` to the host data directory. Other paths and custom
receiver ports stay as configured. Collector output goes to
`~/.lotel/collector.log`. Both backends use a managed lotel process with a private
Unix control socket. Docker mode adopts a matching running Collector without
restarting it. Repeated starts keep the same runtime.

### Automatic ingestion

Both backends ingest at startup, every two seconds by default, and after
exporters flush during shutdown. Database operations use one process lock and
release DuckDB after each command or ingestion pass. Concurrent queries, manual
ingestion, rebuilds, and pruning wait for their turn. CLI database waits time
out after 30 seconds. A background pass retries on the next interval.

`lotel status` includes an `ingestion` object with `enabled`, `running`,
`interval`, `last_success`, and `last_error`. The first successful pass sets
`last_success`. An ingestion error appears in status and the Collector log.
Collection continues after ingestion errors, including errors present at startup.
`lotel start` accepts a ready Collector; `lotel start --wait` also waits for
ingestion health and leaves collection running if that health check times out.
`lotel health` fails while an
enabled worker reports an error or is unavailable. A successful pass clears the
error. Ingestion latency also includes SDK export and Collector batch delays.

During shutdown, `stop` waits for exporter flush and final indexing. It retries
temporary database locks for up to five seconds. If final indexing still fails,
the Collector stops and `stop` exits with an error. Raw JSONL remains available.
Run `lotel ingest` after the database becomes available to index those records.

Add this optional section to `./lotel-collector.yaml` to change automatic
ingestion for either backend:

```yaml
ingestion:
  enabled: true
  interval: 30s
```

The interval must be a positive Go duration. Set `enabled: false` to disable
all automatic ingestion, including startup and shutdown. The `ingestion`
section belongs to lotel and is removed before the Collector reads its config.
Manual `lotel ingest` works with either backend.

### Rebuild the index

`lotel ingest --full` replaces indexed telemetry and resets ingestion progress,
then rereads retained JSONL in one transaction. A read or storage error rolls the
transaction back. Malformed records follow the normal ingestion rules and
appear in the report. Raw files stay intact. Previously pruned rows return
when their raw data remains. Indexed rows without retained raw data disappear.
Repeated full rebuilds do not duplicate records.

The comparison with [lotel-rs](https://github.com/mattsp1290/lotel-rs/tree/77270b2971422571434b2dc77e38581b1da89618)
identified native collection, periodic ingestion, and full rebuilding as the
missing non-embedding capabilities. Go already provides the corresponding
query, aggregation, and retention commands, plus GenAI and attribute filters.

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

After your app makes calls, query automatically indexed telemetry:

```bash
lotel query genai calls --service my-llm-app --since 10m
lotel query genai calls --service my-llm-app --since 10m --content
lotel query genai usage --service my-llm-app --group-by provider,model
lotel query logs --event-name gen_ai.client.inference.operation.details --since 10m
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
lotel stop
lotel start --wait
```

These recipes process span attributes, span events, log attributes, and structured
GenAI event bodies after normalization. They cover native, OpenInference, and
OpenLLMetry content keys. They preserve token counts.

GenAI conventions have Development status. Lotel stores attributes verbatim and
resolves current and legacy aliases listed in
[genai.go](internal/storage/genai.go). It does not estimate monetary cost.

## Verification

The native CLI check uses temporary data, isolated ports, and a PATH without
Docker. It exercises HTTP JSON, HTTP protobuf, gRPC, automatic ingestion,
GenAI queries, restarts, shutdown flush, and full rebuilding:

```bash
go build -o lotel ./cmd/lotel
LOTEL_NATIVE_TEST_BIN="$PWD/lotel" go test ./internal/collector -run '^TestNativeCLI$' -count=1 -v
```

For Docker verification, start Docker first. These checks need no LLM credentials:

```bash
go test ./...
go build ./...
go vet ./...
go build -o lotel ./cmd/lotel
pip install requests
LOTEL_BIN=./lotel python3 scripts/verify.py
./lotel start --wait
LOTEL_BIN=./lotel python3 scripts/verify_otlp.py
LOTEL_DOCKER_TESTS=1 go test ./internal/config -run 'TestExampleConfigsValidate|TestRedactionRecipesBehavior' -v
```

`verify_otlp.py` sends all four signals, compares queried envelopes with the
Collector's actual JSONL, and checks exact numbers, exemplars, all five metric
types, profile queries, and repeated-ingest idempotency. It leaves the Collector
running and retains its uniquely tagged verification data.

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

## Runtime requirements

- Docker for the default backend. Native collection runs on macOS or Linux.
- A supported Linux or macOS host. See [release platform requirements](docs/release.md).

Prebuilt installations do not require Go or a compiler. Source builds need Go
1.26 or later and CGO for DuckDB. See the [build requirements](#build-from-source).

## License

MIT
