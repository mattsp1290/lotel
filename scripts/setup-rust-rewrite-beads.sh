#!/bin/bash
# Project: lotel Go-to-Rust Rewrite
# Generated: 2026-03-21
#
# Creates a comprehensive Beads task graph for rewriting lotel from Go to Rust.
# Eliminates Docker dependency with a native Rust OTel collector, provides an
# importable library crate, and maintains full CLI feature parity.
#
# Upstream OTel Collector parity commit: fdadab8a8
#
# 30 beads across 9 phases, 4 parallel execution tracks
# Each bead targets < 750 lines of code

set -e

cd /home/infra-admin/git/lotel

echo "Creating lotel Go-to-Rust rewrite task graph..."
echo ""

# ==========================================================================
# Phase 1: Workspace Setup & Infrastructure
# ==========================================================================

SETUP_WORKSPACE=$(bd create \
  "Initialize Rust workspace with Cargo.toml and three crate stubs" \
  -p 0 --label setup \
  -d "Create the Rust workspace structure:
- Root Cargo.toml (workspace) with members: crates/lotel-collector, crates/lotel-storage, crates/lotel-cli
- Workspace dependencies section with shared versions:
  tokio = { version = \"1\", features = [\"full\"] }
  tonic = \"0.13\"
  prost = \"0.13\"
  axum = \"0.8\"
  duckdb = { version = \"1\", features = [\"bundled\", \"chrono\"] }
  opentelemetry-proto = { version = \"0.31\", features = [\"gen-tonic\", \"trace\", \"metrics\", \"logs\", \"with-serde\"] }
  clap = { version = \"4\", features = [\"derive\"] }
  serde = { version = \"1\", features = [\"derive\"] }
  serde_json = \"1\"
  serde_yaml = \"0.9\"
  chrono = { version = \"0.4\", features = [\"serde\"] }
  thiserror = \"2\"
  anyhow = \"1\"
  tracing = \"0.1\"
  tokio-util = { version = \"0.7\", features = [\"rt\"] }
- crates/lotel-collector/Cargo.toml: tonic, prost, axum, tokio, serde, serde_json, serde_yaml, opentelemetry-proto, tracing, chrono, tokio-util, thiserror
- crates/lotel-storage/Cargo.toml: duckdb, serde, serde_json, chrono, thiserror, anyhow
- crates/lotel-cli/Cargo.toml: clap, serde_json, tokio, lotel-collector (path), lotel-storage (path), chrono, anyhow
- Each crate gets a minimal lib.rs or main.rs that compiles
- Verify: cargo build --workspace succeeds with stub crates

File reservations: Cargo.toml, crates/*/Cargo.toml, crates/*/src/lib.rs, crates/lotel-cli/src/main.rs" \
  --silent)
echo "SETUP_WORKSPACE=$SETUP_WORKSPACE"

SETUP_CI=$(bd create \
  "Add CI configuration and Rust toolchain pinning" \
  -p 1 --label setup \
  -d "Set up development infrastructure:
- rust-toolchain.toml pinning stable Rust (edition 2021 or 2024)
- .github/workflows/rust.yml: cargo build, cargo test, cargo clippy, cargo fmt --check
- Add rustfmt.toml with project formatting preferences
- Add workspace-level clippy lints in Cargo.toml ([workspace.lints.clippy])
- Update .gitignore for Rust artifacts (target/, *.rs.bk)
- Verify: cargo fmt --check and cargo clippy pass on stub workspace

File reservations: rust-toolchain.toml, .github/workflows/rust.yml, rustfmt.toml, .gitignore" \
  --silent)
echo "SETUP_CI=$SETUP_CI"
bd dep add "$SETUP_CI" "$SETUP_WORKSPACE"

SETUP_PROTO=$(bd create \
  "Validate opentelemetry-proto crate provides needed OTLP types and create PARITY.md" \
  -p 0 --label setup \
  -d "Verify the opentelemetry-proto Rust crate (with gen-tonic + with-serde features) provides:
- ExportTraceServiceRequest / ResourceSpans / ScopeSpans / Span
- ExportMetricsServiceRequest / ResourceMetrics / ScopeMetrics / Metric (Sum, Gauge, Histogram)
- ExportLogsServiceRequest / ResourceLogs / ScopeLogs / LogRecord
- AnyValue, KeyValue (attributes)
- gRPC server traits: TraceServiceServer, MetricsServiceServer, LogsServiceServer
- Serde support for JSON deserialization on HTTP path

Write a small test in lotel-collector that imports and instantiates key types.
Create PARITY.md tracking upstream OTel Collector commit fdadab8a8.

File reservations: crates/lotel-collector/src/proto_check.rs, PARITY.md" \
  --silent)
echo "SETUP_PROTO=$SETUP_PROTO"
bd dep add "$SETUP_PROTO" "$SETUP_WORKSPACE"

# ==========================================================================
# Phase 2: Core Data Model & Shared Types
# ==========================================================================

DATA_MODEL=$(bd create \
  "Define internal telemetry data model types shared across crates" \
  -p 0 --label core \
  -d "Create internal data model bridging OTLP proto types and DuckDB storage.

Create crates/lotel-collector/src/model.rs:
- SpanRecord: trace_id, span_id, parent_span_id (Option), name, kind (i32), start_time (DateTime), end_time (Option<DateTime>), duration_ns (i64), status_code (i32), service_name, attributes (serde_json::Value)
- MetricRecord: metric_name, metric_type (enum: Sum/Gauge/Histogram as string), value (f64), timestamp (DateTime), service_name, aggregation_temporality (Option<i32>), is_monotonic (Option<bool>), unit (Option<String>), attributes (serde_json::Value)
- LogRecord: timestamp (DateTime), severity (Option<String>), severity_number (Option<i32>), body (Option<String>), service_name, trace_id (Option), span_id (Option), attributes (serde_json::Value)
- Serde Serialize/Deserialize on all types for JSON output
- OtlpNano custom deserializer: handles nanosecond timestamps as either string or integer (matching Go otlpNano in internal/storage/ingest.go line 247)
- Helper: extract_service_name(resource_attributes) -> String (defaults to 'unknown')
- Helper: flatten_attrs(Vec<KeyValue>) -> serde_json::Value

Reference: internal/storage/ingest.go lines 247-430 for OTLP JSON structures

File reservations: crates/lotel-collector/src/model.rs" \
  --silent)
echo "DATA_MODEL=$DATA_MODEL"
bd dep add "$DATA_MODEL" "$SETUP_PROTO"

CONFIG_TYPES=$(bd create \
  "Implement config types and YAML parsing for collector configuration" \
  -p 0 --label core \
  -d "Implement the configuration layer matching Go internal/config/config.go.

Create crates/lotel-collector/src/config.rs:
- CollectorConfig struct with serde YAML deserialization:
  - receivers.otlp.protocols.grpc.endpoint (default 0.0.0.0:4317)
  - receivers.otlp.protocols.http.endpoint (default 0.0.0.0:4318)
  - processors.batch.timeout (default 1s), send_batch_size (1024), send_batch_max_size (2048)
  - exporters: file/traces, file/metrics, file/logs with path and format fields
  - extensions.health_check.endpoint (default 0.0.0.0:13133)
  - service.extensions list, service.pipelines map
- DEFAULT_CONFIG: embedded YAML string matching Go DefaultConfig exactly
  - Paths use ~/.lotel/data/ instead of /data/ (no Docker volume mapping)
- resolve_config_path() -> PathBuf:
  1. Check CWD for lotel-collector.yaml
  2. Fall back to ~/.lotel/collector-config.yaml
  3. Create default if absent
- data_path() -> PathBuf: returns ~/.lotel/data/
- Unit tests for YAML parsing of default config

Reference: internal/config/config.go (113 lines)

File reservations: crates/lotel-collector/src/config.rs" \
  --silent)
echo "CONFIG_TYPES=$CONFIG_TYPES"
bd dep add "$CONFIG_TYPES" "$SETUP_WORKSPACE"

# ==========================================================================
# Phase 3: Collector Components
# ==========================================================================

RECEIVER_GRPC=$(bd create \
  "Implement OTLP gRPC receiver on port 4317" \
  -p 0 --label collector \
  -d "Implement the gRPC OTLP receiver using tonic.

Create crates/lotel-collector/src/receiver/mod.rs and grpc.rs:
- Implement OTLP gRPC service traits from opentelemetry-proto (gen-tonic feature):
  - trace_service_server::TraceService::export(ExportTraceServiceRequest) -> ExportTraceServiceResponse
  - metrics_service_server::MetricsService::export(ExportMetricsServiceRequest) -> ExportMetricsServiceResponse
  - logs_service_server::LogsService::export(ExportLogsServiceRequest) -> ExportLogsServiceResponse
- Each handler forwards received proto request through a tokio::mpsc::Sender
- OtlpGrpcReceiver struct with:
  - new(endpoint: SocketAddr, tx: mpsc::Sender<SignalData>) -> Self
  - serve(self, cancel: CancellationToken) -> Result<()>
- SignalData enum: Traces(ExportTraceServiceRequest), Metrics(...), Logs(...)
- Return empty response on success, gRPC status codes on error
- Graceful shutdown via CancellationToken
- Unit tests: create tonic client, send minimal request, verify channel receives it

File reservations: crates/lotel-collector/src/receiver/" \
  --silent)
echo "RECEIVER_GRPC=$RECEIVER_GRPC"
bd dep add "$RECEIVER_GRPC" "$DATA_MODEL"
bd dep add "$RECEIVER_GRPC" "$CONFIG_TYPES"

RECEIVER_HTTP=$(bd create \
  "Implement OTLP HTTP receiver on port 4318" \
  -p 0 --label collector \
  -d "Implement the OTLP HTTP receiver using axum.

Create crates/lotel-collector/src/receiver/http.rs:
- POST /v1/traces: accept JSON body (ExportTraceServiceRequest), forward to channel
- POST /v1/metrics: accept JSON body (ExportMetricsServiceRequest), forward to channel
- POST /v1/logs: accept JSON body (ExportLogsServiceRequest), forward to channel
- Content-Type: application/json (required for verify.py compatibility)
- Optional: application/x-protobuf support via prost::Message::decode
- Return HTTP 200 with empty JSON body on success
- Return 400 for malformed requests, 500 for internal errors
- OtlpHttpReceiver struct with:
  - new(endpoint: SocketAddr, tx: mpsc::Sender<SignalData>) -> Self
  - serve(self, cancel: CancellationToken) -> Result<()>
- Share the same mpsc::Sender as gRPC receiver (unified data path)
- Graceful shutdown via axum::serve with_graceful_shutdown

The HTTP endpoint is what verify.py uses:
  requests.post(f'{OTLP_HTTP}/v1/traces', json=data)
  requests.post(f'{OTLP_HTTP}/v1/metrics', json=data)
  requests.post(f'{OTLP_HTTP}/v1/logs', json=data)

File reservations: crates/lotel-collector/src/receiver/http.rs" \
  --silent)
echo "RECEIVER_HTTP=$RECEIVER_HTTP"
bd dep add "$RECEIVER_HTTP" "$DATA_MODEL"
bd dep add "$RECEIVER_HTTP" "$CONFIG_TYPES"

BATCH_PROCESSOR=$(bd create \
  "Implement batch processor with configurable timeout and size limits" \
  -p 0 --label collector \
  -d "Implement the batch processor matching OTel Collector batch semantics.

Create crates/lotel-collector/src/processor/mod.rs and batch.rs:
- BatchProcessor struct:
  - config: timeout (Duration), send_batch_size (usize), send_batch_max_size (usize)
  - Defaults: timeout=1s, send_batch_size=1024, send_batch_max_size=2048
- Receives SignalData from receiver channel (tokio::mpsc::Receiver)
- Accumulates items in internal buffer
- Flushes to exporter channel (tokio::mpsc::Sender) when:
  1. Buffer reaches send_batch_size items, OR
  2. Timeout expires since last flush (tokio::time::interval)
  3. Buffer exceeds send_batch_max_size -> immediate flush
- BatchProcessor::run(self, rx: Receiver, tx: Sender, cancel: CancellationToken) -> Result<()>
- Graceful shutdown: on cancel signal, flush remaining buffered items
- Handle all three signal types (Traces, Metrics, Logs) through the same processor
- Unit tests:
  - Send exactly send_batch_size items, verify flush
  - Send fewer items, wait for timeout, verify flush
  - Verify shutdown flushes remaining items

File reservations: crates/lotel-collector/src/processor/" \
  --silent)
echo "BATCH_PROCESSOR=$BATCH_PROCESSOR"
bd dep add "$BATCH_PROCESSOR" "$DATA_MODEL"

FILE_EXPORTER=$(bd create \
  "Implement JSONL file exporter for traces, metrics, and logs" \
  -p 0 --label collector \
  -d "Implement the file exporter that writes OTLP data as JSONL.

Create crates/lotel-collector/src/exporter/mod.rs and file.rs:
- FileExporter struct:
  - traces_path: PathBuf (default ~/.lotel/data/traces/traces.jsonl)
  - metrics_path: PathBuf (default ~/.lotel/data/metrics/metrics.jsonl)
  - logs_path: PathBuf (default ~/.lotel/data/logs/logs.jsonl)
- FileExporter::run(self, rx: Receiver<SignalData>, cancel: CancellationToken) -> Result<()>
- On receiving SignalData:
  - Match on variant (Traces/Metrics/Logs)
  - Serialize the proto request to JSON using serde (with-serde feature)
  - Append as single line to appropriate JSONL file (open in append mode)
  - Each line must be valid JSON ending with newline
- Output format must match OTel Collector file exporter:
  - Traces: {\"resourceSpans\":[...]}
  - Metrics: {\"resourceMetrics\":[...]}
  - Logs: {\"resourceLogs\":[...]}
- Create parent directories if absent (std::fs::create_dir_all)
- Use BufWriter for performance
- Flush on shutdown
- Unit tests: write test data, read back, verify valid JSONL

Reference: Go config paths in internal/config/config.go

File reservations: crates/lotel-collector/src/exporter/" \
  --silent)
echo "FILE_EXPORTER=$FILE_EXPORTER"
bd dep add "$FILE_EXPORTER" "$DATA_MODEL"
bd dep add "$FILE_EXPORTER" "$CONFIG_TYPES"

HEALTH_CHECK=$(bd create \
  "Implement health check HTTP extension on port 13133" \
  -p 1 --label collector \
  -d "Implement the health check extension.

Create crates/lotel-collector/src/extension/mod.rs and health.rs:
- HealthCheckExtension struct:
  - endpoint: SocketAddr (default 0.0.0.0:13133)
  - ready: Arc<AtomicBool> (shared with pipeline orchestrator)
- HTTP server (axum) serving:
  - GET / -> 200 OK when ready.load() is true
  - GET / -> 503 Service Unavailable when not ready
- HealthCheckExtension::run(self, cancel: CancellationToken) -> Result<()>
- The ready flag is set to true by the pipeline after all components are started

This endpoint is polled by:
- lotel health command (HTTP GET http://localhost:13133/)
- lotel start --wait (polls with 500ms interval, 30s timeout)
- verify.py check_collector_health()

File reservations: crates/lotel-collector/src/extension/" \
  --silent)
echo "HEALTH_CHECK=$HEALTH_CHECK"
bd dep add "$HEALTH_CHECK" "$CONFIG_TYPES"

# ==========================================================================
# Phase 4: Pipeline Orchestration & Library API
# ==========================================================================

PIPELINE=$(bd create \
  "Implement pipeline orchestration connecting receivers -> processor -> exporters" \
  -p 0 --label collector \
  -d "Wire all collector components into a running pipeline.

Create crates/lotel-collector/src/pipeline.rs:
- Pipeline struct owning all components and channels
- Pipeline::new(config: &CollectorConfig) -> Result<Self>:
  - Create tokio::mpsc channels (receiver->processor, processor->exporter)
  - Instantiate OtlpGrpcReceiver (bound to config gRPC endpoint)
  - Instantiate OtlpHttpReceiver (bound to config HTTP endpoint)
  - Instantiate BatchProcessor (with config batch params)
  - Instantiate FileExporter (with config paths)
  - Instantiate HealthCheckExtension (with config endpoint, shared Arc<AtomicBool>)
  - Create shared CancellationToken for coordinated shutdown
- Pipeline::run(self) -> Result<PipelineHandle>:
  - Spawn each component as a tokio task via tokio::spawn
  - Set health check readiness to true after all tasks spawned
  - Return PipelineHandle with CancellationToken and JoinHandle vec
- PipelineHandle::shutdown(self) -> Result<()>:
  - Cancel the CancellationToken
  - Await all JoinHandles for graceful drain
- Error propagation: if any component task panics/errors, cancel all others

File reservations: crates/lotel-collector/src/pipeline.rs" \
  --silent)
echo "PIPELINE=$PIPELINE"
bd dep add "$PIPELINE" "$RECEIVER_GRPC"
bd dep add "$PIPELINE" "$RECEIVER_HTTP"
bd dep add "$PIPELINE" "$BATCH_PROCESSOR"
bd dep add "$PIPELINE" "$FILE_EXPORTER"
bd dep add "$PIPELINE" "$HEALTH_CHECK"

LIBRARY_API=$(bd create \
  "Implement public library API for lotel-collector crate" \
  -p 0 --label collector \
  -d "Define the public API surface of lotel-collector as an importable library.

Update crates/lotel-collector/src/lib.rs:
- pub mod config, model, receiver, processor, exporter, extension, pipeline
- pub struct Collector { config: CollectorConfig }
- Collector::new(config: CollectorConfig) -> Result<Self>
- Collector::from_config_file(path: &Path) -> Result<Self>
- Collector::with_defaults() -> Result<Self>
- collector.start(self) -> Result<CollectorHandle>
  - Builds Pipeline from config
  - Calls pipeline.run()
  - Returns CollectorHandle wrapping PipelineHandle
- pub struct CollectorHandle { ... }
- CollectorHandle::shutdown(self) -> Result<()>
- CollectorHandle::wait_healthy(&self, timeout: Duration) -> Result<()>
  - Polls health endpoint with 500ms interval (matching Go WaitHealthy)
- CollectorHandle::is_healthy(&self) -> bool
  - HTTP GET to health endpoint
- CollectorHandle::status(&self) -> CollectorStatus
- pub struct CollectorStatus { running: bool, healthy: bool, uptime: Duration, config_path: PathBuf, data_path: PathBuf }
- Re-export CollectorConfig and key types

Usage example (for multisamples):
  let collector = Collector::with_defaults()?;
  let handle = collector.start().await?;
  handle.wait_healthy(Duration::from_secs(30)).await?;
  // ... do work ...
  handle.shutdown().await?;

File reservations: crates/lotel-collector/src/lib.rs" \
  --silent)
echo "LIBRARY_API=$LIBRARY_API"
bd dep add "$LIBRARY_API" "$PIPELINE"

# ==========================================================================
# Phase 5: DuckDB Storage Layer
# ==========================================================================

DB_SCHEMA=$(bd create \
  "Implement DuckDB connection management and schema migration" \
  -p 0 --label storage \
  -d "Implement the DuckDB storage foundation in lotel-storage.

Create crates/lotel-storage/src/db.rs:
- pub fn open_db(path: &Path) -> Result<Connection>
  - Open DuckDB at given path, run migration
  - Create parent directories if absent
- pub fn default_db() -> Result<Connection>
  - Opens at ~/.lotel/data/lotel.db
- fn migrate(conn: &Connection) -> Result<()>
  - CREATE TABLE IF NOT EXISTS traces (
      trace_id VARCHAR NOT NULL, span_id VARCHAR NOT NULL, parent_span_id VARCHAR,
      name VARCHAR NOT NULL, kind INTEGER, start_time TIMESTAMP NOT NULL,
      end_time TIMESTAMP, duration_ns BIGINT, status_code INTEGER,
      service_name VARCHAR NOT NULL, attributes JSON, date DATE NOT NULL)
  - CREATE TABLE IF NOT EXISTS metrics (
      metric_name VARCHAR NOT NULL, metric_type VARCHAR NOT NULL, value DOUBLE,
      timestamp TIMESTAMP NOT NULL, service_name VARCHAR NOT NULL,
      aggregation_temporality INTEGER, is_monotonic BOOLEAN, unit VARCHAR,
      attributes JSON, date DATE NOT NULL)
  - CREATE TABLE IF NOT EXISTS logs (
      timestamp TIMESTAMP NOT NULL, severity VARCHAR, severity_number INTEGER,
      body VARCHAR, service_name VARCHAR NOT NULL, trace_id VARCHAR,
      span_id VARCHAR, attributes JSON, date DATE NOT NULL)
- Unit tests: open in-memory DB, run migration, verify tables exist via information_schema

Reference: internal/storage/db.go (108 lines) - exact schema match

File reservations: crates/lotel-storage/src/db.rs, crates/lotel-storage/src/lib.rs" \
  --silent)
echo "DB_SCHEMA=$DB_SCHEMA"
bd dep add "$DB_SCHEMA" "$SETUP_WORKSPACE"

INGEST_TRACES=$(bd create \
  "Implement JSONL traces ingestion into DuckDB" \
  -p 1 --label storage \
  -d "Implement traces ingestion from OTLP JSONL files into DuckDB.

Create crates/lotel-storage/src/ingest.rs (traces portion):
- pub fn ingest_all(conn: &Connection, data_path: &Path) -> Result<()>
  - Calls ingest_traces, ingest_metrics, ingest_logs for respective subdirs
  - Skips missing files/directories gracefully
- fn ingest_traces(conn: &Connection, file: &Path) -> Result<()>
  - Read JSONL file line by line (BufReader with large buffer)
  - Parse each line as serde_json::Value with 'resourceSpans' array
  - For each resourceSpan -> scopeSpan -> span:
    - Extract service.name from resource.attributes (key='service.name', value.stringValue)
    - Parse startTimeUnixNano and endTimeUnixNano (handle both string and integer formats)
    - Compute duration_ns = end - start
    - Flatten span attributes to JSON
    - Extract: traceId, spanId, parentSpanId, name, kind, status.code
  - INSERT INTO traces (trace_id, span_id, ..., date) VALUES (?, ?, ..., ?)
  - Use transaction (BEGIN/COMMIT) for batch performance
  - Skip malformed lines (log warning, continue)
- OTLP nanosecond timestamp: deserialize as either string or integer (u64)
  - Convert to chrono::NaiveDateTime
  - Extract date component for partition key

Reference: internal/storage/ingest.go lines 38-115 (ingestTraces)

File reservations: crates/lotel-storage/src/ingest.rs" \
  --silent)
echo "INGEST_TRACES=$INGEST_TRACES"
bd dep add "$INGEST_TRACES" "$DB_SCHEMA"

INGEST_METRICS=$(bd create \
  "Implement JSONL metrics ingestion into DuckDB" \
  -p 1 --label storage \
  -d "Add metrics ingestion to crates/lotel-storage/src/ingest.rs:
- fn ingest_metrics(conn: &Connection, file: &Path) -> Result<()>
  - Read JSONL file line by line
  - Parse each line with 'resourceMetrics' array
  - For each resourceMetric -> scopeMetric -> metric:
    - Extract service.name from resource.attributes
    - Handle metric types:
      - Sum: metric_type='sum', dataPoints array, aggregationTemporality (int), isMonotonic (bool)
        - Value from dataPoints[].asDouble or asInt (asInt is a string like '1')
      - Gauge: metric_type='gauge', dataPoints array
        - Value from dataPoints[].asDouble or asInt
      - Histogram: metric_type='histogram', dataPoints array
        - Value from dataPoints[].sum
    - Parse timeUnixNano from each dataPoint
    - Extract unit from metric.unit
    - Flatten dataPoint attributes
  - INSERT INTO metrics with all fields
  - Transaction wrapping
  - Skip malformed lines

Reference: internal/storage/ingest.go lines 117-177 and 306-405

File reservations: crates/lotel-storage/src/ingest.rs (metrics section)" \
  --silent)
echo "INGEST_METRICS=$INGEST_METRICS"
bd dep add "$INGEST_METRICS" "$DB_SCHEMA"

INGEST_LOGS=$(bd create \
  "Implement JSONL logs ingestion into DuckDB" \
  -p 1 --label storage \
  -d "Add logs ingestion to crates/lotel-storage/src/ingest.rs:
- fn ingest_logs(conn: &Connection, file: &Path) -> Result<()>
  - Read JSONL file line by line
  - Parse each line with 'resourceLogs' array
  - For each resourceLog -> scopeLog -> logRecord:
    - Extract service.name from resource.attributes
    - Parse timeUnixNano (string or integer)
    - Extract severityText, severityNumber
    - Extract body from body.stringValue
    - Extract traceId, spanId (optional, for trace correlation)
    - Flatten logRecord attributes to JSON
  - INSERT INTO logs with all fields
  - Transaction wrapping
  - Skip malformed lines

Reference: internal/storage/ingest.go lines 179-245

File reservations: crates/lotel-storage/src/ingest.rs (logs section)" \
  --silent)
echo "INGEST_LOGS=$INGEST_LOGS"
bd dep add "$INGEST_LOGS" "$DB_SCHEMA"

QUERY_IMPL=$(bd create \
  "Implement query interface for traces, metrics, logs, and aggregation" \
  -p 1 --label storage \
  -d "Implement the query layer matching all Go query functions.

Create crates/lotel-storage/src/query.rs:
- pub struct QueryOptions { service: Option<String>, since: Option<NaiveDateTime>, until: Option<NaiveDateTime>, limit: Option<usize> }
- pub struct TraceResult { trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes } (all Serialize)
- pub struct MetricResult { metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, attributes } (all Serialize)
- pub struct LogResult { timestamp, severity, severity_number, body, service_name, trace_id, span_id, attributes } (all Serialize)
- pub struct MetricAggregation { metric_name, service_name, count (i64), avg (Option<f64>), min (Option<f64>), max (Option<f64>) }

Implement:
- pub fn query_traces(conn, opts) -> Result<Vec<TraceResult>>
  SQL: SELECT ... FROM traces WHERE 1=1 [AND service_name = ?] [AND start_time >= ?] [AND start_time <= ?] ORDER BY start_time ASC [LIMIT ?]
- pub fn query_metrics(conn, opts) -> Result<Vec<MetricResult>>
  SQL: same pattern on metrics table, filter on timestamp
- pub fn query_logs(conn, opts) -> Result<Vec<LogResult>>
  SQL: same pattern on logs table, filter on timestamp
- pub fn aggregate_metrics(conn, opts, metric_name: &str) -> Result<MetricAggregation>
  SQL: SELECT COUNT(*), AVG(value), MIN(value), MAX(value) FROM metrics WHERE metric_name = ? [AND service_name = ?] [AND timestamp >= ?]
- fn build_where(time_col: &str, opts: &QueryOptions) -> (String, Vec<Box<dyn ToSql>>)
  Dynamic WHERE clause builder matching Go buildWhere

Reference: internal/storage/query.go (249 lines)

File reservations: crates/lotel-storage/src/query.rs" \
  --silent)
echo "QUERY_IMPL=$QUERY_IMPL"
bd dep add "$QUERY_IMPL" "$DB_SCHEMA"

PRUNE_IMPL=$(bd create \
  "Implement time-based data pruning with dry-run support" \
  -p 1 --label storage \
  -d "Implement the prune functionality matching Go Prune.

Create crates/lotel-storage/src/prune.rs:
- pub struct PruneReport { signal: String, service_name: Option<String>, deleted: i64, cutoff: String } (Serialize)
- pub fn prune(conn, cutoff: NaiveDateTime, service: Option<&str>, dry_run: bool) -> Result<Vec<PruneReport>>
  - For each signal (traces, metrics, logs):
    - Time column: traces->start_time, metrics->timestamp, logs->timestamp
    - SQL: SELECT COUNT(*) FROM {table} WHERE {time_col} < ? [AND service_name = ?]
    - If not dry_run and count > 0: DELETE FROM {table} WHERE {time_col} < ? [AND service_name = ?]
    - Build PruneReport with signal name, service, count, cutoff as RFC3339
  - Return Vec of 3 PruneReport entries
- Unit tests:
  - Insert test data, prune with dry_run, verify no deletion
  - Prune for real, verify deletion
  - Prune with service filter

Reference: internal/storage/prune.go (67 lines)

File reservations: crates/lotel-storage/src/prune.rs" \
  --silent)
echo "PRUNE_IMPL=$PRUNE_IMPL"
bd dep add "$PRUNE_IMPL" "$DB_SCHEMA"

STORAGE_LIB=$(bd create \
  "Wire up lotel-storage lib.rs with public API exports" \
  -p 1 --label storage \
  -d "Finalize the lotel-storage crate public API.

Update crates/lotel-storage/src/lib.rs:
- pub mod db;
- pub mod ingest;
- pub mod query;
- pub mod prune;
- Re-export key types at crate root:
  - QueryOptions, TraceResult, MetricResult, LogResult, MetricAggregation (from query)
  - PruneReport (from prune)
- Re-export key functions:
  - open_db, default_db (from db)
  - ingest_all (from ingest)
  - query_traces, query_metrics, query_logs, aggregate_metrics (from query)
  - prune (from prune)
- Verify: cargo doc --no-deps generates clean documentation

File reservations: crates/lotel-storage/src/lib.rs" \
  --silent)
echo "STORAGE_LIB=$STORAGE_LIB"
bd dep add "$STORAGE_LIB" "$INGEST_TRACES"
bd dep add "$STORAGE_LIB" "$INGEST_METRICS"
bd dep add "$STORAGE_LIB" "$INGEST_LOGS"
bd dep add "$STORAGE_LIB" "$QUERY_IMPL"
bd dep add "$STORAGE_LIB" "$PRUNE_IMPL"

# ==========================================================================
# Phase 6: CLI Implementation
# ==========================================================================

CLI_SCAFFOLD=$(bd create \
  "Implement CLI scaffold with clap and all subcommand stubs" \
  -p 0 --label cli \
  -d "Create the CLI binary matching the Go command structure.

Create crates/lotel-cli/src/main.rs:
- Use clap derive macros
- Top-level Cli struct with subcommands:
  - Start { wait: bool }
  - Stop
  - Status
  - Health
  - Ingest
  - Query { subcommand: QueryCommand }
  - Prune { older_than: Option<String>, service: Option<String>, dry_run: bool, all: bool }
  - RunCollector { config: PathBuf, data: PathBuf } (hidden, for daemon self-spawn)
- QueryCommand enum:
  - Traces { service: Option<String>, since: Option<String>, until: Option<String>, limit: Option<usize> }
  - Metrics { same flags }
  - Logs { same flags }
  - Aggregate { metric: String, service: Option<String>, since: Option<String>, until: Option<String> }

Create crates/lotel-cli/src/time.rs:
- parse_time(s: &str) -> Result<NaiveDateTime>: try RFC3339, then relative duration
- parse_duration(s: &str) -> Result<Duration>: support 'Nd' for days (7d, 1d), then standard h/m/s

Create helper fn print_json<T: Serialize>(value: &T): pretty-print JSON to stdout

Each subcommand handler is a stub returning Ok(()) for now.
All output goes to stdout as JSON. Errors to stderr. Exit 0/1.

Reference: cmd/lotel/main.go (333 lines)

File reservations: crates/lotel-cli/src/" \
  --silent)
echo "CLI_SCAFFOLD=$CLI_SCAFFOLD"
bd dep add "$CLI_SCAFFOLD" "$SETUP_WORKSPACE"

CLI_COLLECTOR_CMDS=$(bd create \
  "Implement CLI start, stop, status, and health commands" \
  -p 0 --label cli \
  -d "Wire collector lifecycle commands into the CLI.

Update crates/lotel-cli/src/main.rs (collector handlers):

start command:
- Resolve config path via lotel_collector::config::resolve_config_path()
- Resolve data path via lotel_collector::config::data_path()
- Check for stale state, clean up dead PID
- Spawn self as daemon: Command::new(current_exe()).arg('run-collector').arg('--config').arg(path).arg('--data').arg(data)
  - Redirect stdout/stderr to ~/.lotel/collector.log
  - Detach (platform-specific: .spawn() + don't wait)
- Write state file ~/.lotel/collector.state: { pid, started_at, config_path, data_path }
- If --wait: poll http://localhost:13133/ every 500ms for up to 30s
- Print startup info

run-collector (hidden) command:
- Parse config from --config flag
- Create Collector::from_config_file(), call start().await
- Block until SIGTERM/SIGINT (tokio::signal)
- Call handle.shutdown()

stop command:
- Read PID from ~/.lotel/collector.state
- Send SIGTERM (nix::sys::signal::kill or std::process::Command kill)
- Wait for process exit (poll /proc/PID or waitpid with timeout)
- Remove state file
- Print 'Collector stopped.'

status command:
- Read state file, check PID alive, check health endpoint
- Output JSON: { running, healthy, uptime, pid }
- Exit code 1 if not running

health command:
- HTTP GET http://localhost:13133/ with 2s timeout
- Exit 0 if 200 OK, exit 1 otherwise

Reference: internal/collector/collector.go (295 lines)

File reservations: crates/lotel-cli/src/main.rs, crates/lotel-cli/src/daemon.rs" \
  --silent)
echo "CLI_COLLECTOR_CMDS=$CLI_COLLECTOR_CMDS"
bd dep add "$CLI_COLLECTOR_CMDS" "$CLI_SCAFFOLD"
bd dep add "$CLI_COLLECTOR_CMDS" "$LIBRARY_API"
bd dep add "$CLI_COLLECTOR_CMDS" "$CONFIG_TYPES"

CLI_STORAGE_CMDS=$(bd create \
  "Implement CLI ingest, query, and prune commands" \
  -p 1 --label cli \
  -d "Wire storage commands into the CLI.

Update crates/lotel-cli/src/main.rs (storage handlers):

ingest command:
- Get data_path from config::data_path()
- Open DB via lotel_storage::default_db()
- Call lotel_storage::ingest_all(conn, data_path)
- Print 'Ingestion complete.'

query traces command:
- Open DB
- Parse --service, --since (parse_time), --until (parse_time), --limit into QueryOptions
- Call lotel_storage::query_traces(conn, opts)
- print_json(&results)

query metrics command:
- Same pattern, call query_metrics

query logs command:
- Same pattern, call query_logs

query aggregate command:
- Require --metric flag (eprintln error + exit 1 if missing)
- Parse QueryOptions (no limit for aggregation)
- Call lotel_storage::aggregate_metrics(conn, opts, &metric_name)
- print_json(&result)

prune command:
- Validate: exactly one of --all or --older-than required
- Parse cutoff:
  - --older-than: now - parse_duration(value)
  - --all: far future timestamp (effectively delete everything)
- Call lotel_storage::prune(conn, cutoff, service.as_deref(), dry_run)
- If dry_run: eprintln('Dry run -- no data was deleted.')
- print_json(&reports)

Reference: cmd/lotel/main.go lines 97-280

File reservations: crates/lotel-cli/src/main.rs (storage handlers)" \
  --silent)
echo "CLI_STORAGE_CMDS=$CLI_STORAGE_CMDS"
bd dep add "$CLI_STORAGE_CMDS" "$CLI_SCAFFOLD"
bd dep add "$CLI_STORAGE_CMDS" "$STORAGE_LIB"

# ==========================================================================
# Phase 7: Process Lifecycle & Daemonization
# ==========================================================================

PROCESS_LIFECYCLE=$(bd create \
  "Implement collector process lifecycle for start/stop across CLI invocations" \
  -p 0 --label process \
  -d "Implement the background process lifecycle mechanism.

Create crates/lotel-cli/src/daemon.rs:
- pub struct CollectorState { pid: u32, started_at: String, config_path: String, data_path: String } (Serialize/Deserialize)
- State file path: ~/.lotel/collector.state

Functions:
- pub fn read_state() -> Result<Option<CollectorState>>
  - Read and parse JSON from state file, return None if absent
- pub fn write_state(state: &CollectorState) -> Result<()>
  - Serialize to JSON, write to temp file, rename atomically
- pub fn remove_state() -> Result<()>
  - Remove state file
- pub fn is_pid_alive(pid: u32) -> bool
  - Check /proc/{pid}/cmdline exists and contains 'lotel' (Linux)
  - Fallback: kill(pid, 0) == Ok (any platform with nix crate)
- pub fn stop_process(pid: u32, timeout: Duration) -> Result<()>
  - Send SIGTERM via nix::sys::signal::kill(Pid::from_raw(pid), Signal::SIGTERM)
  - Poll is_pid_alive every 100ms until dead or timeout
  - If still alive after timeout, send SIGKILL
- pub fn cleanup_stale_state() -> Result<()>
  - Read state, check PID alive, remove state if dead
- pub fn spawn_collector(config_path: &Path, data_path: &Path) -> Result<u32>
  - Build Command::new(std::env::current_exe()?)
  - Args: [\"run-collector\", \"--config\", config_path, \"--data\", data_path]
  - Redirect stdout/stderr to ~/.lotel/collector.log (File::create, .stdout(Stdio::from(file)))
  - .spawn() and return child.id()
  - Do NOT call .wait() (let child run independently)

File reservations: crates/lotel-cli/src/daemon.rs" \
  --silent)
echo "PROCESS_LIFECYCLE=$PROCESS_LIFECYCLE"
bd dep add "$PROCESS_LIFECYCLE" "$LIBRARY_API"
bd dep add "$PROCESS_LIFECYCLE" "$CLI_SCAFFOLD"

# ==========================================================================
# Phase 8: Integration Testing & Verification
# ==========================================================================

UNIT_TESTS_COLLECTOR=$(bd create \
  "Add unit tests for collector components (receiver, processor, exporter, health)" \
  -p 1 --label testing \
  -d "Add comprehensive unit tests for each collector component.

crates/lotel-collector tests (inline #[cfg(test)] modules):

receiver tests:
- Test gRPC receiver accepts ExportTraceServiceRequest, verify data arrives on channel
- Test HTTP receiver accepts POST /v1/traces with JSON body
- Test HTTP receiver returns 200 on valid request, 400 on malformed JSON
- Use random ports (TcpListener::bind('127.0.0.1:0')) to avoid conflicts

processor tests:
- Test batch processor flushes at send_batch_size
- Test batch processor flushes on timeout expiry
- Test graceful shutdown flushes remaining items
- Use tokio::time::pause() for deterministic timeout testing

exporter tests:
- Test file exporter creates directories
- Test file exporter writes valid JSONL (each line parses as JSON)
- Test file exporter appends, not overwrites
- Use tempdir for isolation

health check tests:
- Test returns 503 before ready signal
- Test returns 200 after ready signal set
- Use random port

All async tests use #[tokio::test].

File reservations: crates/lotel-collector/src/*/tests.rs or inline #[cfg(test)]" \
  --silent)
echo "UNIT_TESTS_COLLECTOR=$UNIT_TESTS_COLLECTOR"
bd dep add "$UNIT_TESTS_COLLECTOR" "$PIPELINE"

UNIT_TESTS_STORAGE=$(bd create \
  "Add unit tests for storage layer (ingest, query, prune)" \
  -p 1 --label testing \
  -d "Add comprehensive unit tests for storage operations.

crates/lotel-storage tests (inline #[cfg(test)] modules):

Ingestion tests:
- ingest_traces with valid OTLP JSONL (fixture matching verify.py trace format)
- ingest_traces skips malformed lines without erroring
- ingest_metrics with Sum type (asInt as string '1')
- ingest_metrics with Gauge type
- ingest_logs with complete log records
- Nanosecond timestamp parsing: string and integer formats
- extract_service_name returns 'unknown' when missing

Query tests:
- query_traces returns all spans with no filters
- query_traces filters by service_name
- query_traces filters by time range (since/until)
- query_traces respects limit
- query_traces orders by start_time ASC
- query_metrics with filter combinations
- query_logs with nullable fields
- aggregate_metrics returns correct avg/min/max/count
- aggregate_metrics with no matching data returns zero count

Prune tests:
- prune dry_run reports counts without deleting
- prune deletes data older than cutoff
- prune with service filter only deletes matching service

All tests use DuckDB in temp directory via open_db().

File reservations: crates/lotel-storage/tests/ or inline #[cfg(test)]" \
  --silent)
echo "UNIT_TESTS_STORAGE=$UNIT_TESTS_STORAGE"
bd dep add "$UNIT_TESTS_STORAGE" "$STORAGE_LIB"

INTEGRATION_TEST=$(bd create \
  "Add integration test: full pipeline from OTLP send to DuckDB query" \
  -p 1 --label testing \
  -d "Create a programmatic integration test exercising the full pipeline.

Create tests/integration_test.rs (workspace-level):
- Start Collector with test config (random ports for all endpoints)
- Wait for health check to pass
- Send OTLP traces via HTTP POST to /v1/traces (using reqwest)
- Send OTLP metrics via HTTP POST to /v1/metrics
- Send OTLP logs via HTTP POST to /v1/logs
- Use UUID-based service.name for isolation (matching verify.py pattern)
- Wait for batch processor to flush (sleep 2s or poll JSONL file)
- Run ingest_all to load JSONL into DuckDB (temp directory)
- Query traces by service name, assert non-empty
- Query metrics by service name, assert non-empty
- Query logs by service name, assert non-empty
- Run aggregate_metrics, assert count > 0
- Prune with dry_run, assert reports deletions
- Prune for real, assert data is gone
- Shut down collector

Add reqwest as dev-dependency for HTTP client.
Use random ports to avoid conflicts.

File reservations: tests/integration_test.rs, Cargo.toml (dev-deps)" \
  --silent)
echo "INTEGRATION_TEST=$INTEGRATION_TEST"
bd dep add "$INTEGRATION_TEST" "$LIBRARY_API"
bd dep add "$INTEGRATION_TEST" "$STORAGE_LIB"

E2E_VERIFY=$(bd create \
  "Update verify.py for Rust binary and add CLI E2E test" \
  -p 2 --label testing \
  -d "Update scripts/verify.py and add CLI-based E2E test.

1. Update scripts/verify.py:
- LOTEL default: check for target/release/lotel-cli, target/debug/lotel-cli, then 'lotel'
- Keep identical test flow: start -> health -> send OTLP -> ingest -> query -> prune -> stop
- Verify all JSON output parses correctly
- Ensure exit codes match expectations
- Script should work with both Go and Rust binaries (controlled via LOTEL_BIN env var)

2. Optionally create tests/e2e_cli_test.rs:
- Build CLI binary via cargo build
- Run all lotel commands via std::process::Command
- Same flow as verify.py but in Rust
- Parse JSON output, validate structure and content
- Verify exit codes

Acceptance criterion: LOTEL_BIN=./target/release/lotel-cli python3 scripts/verify.py passes

Reference: scripts/verify.py (285 lines)

File reservations: scripts/verify.py, tests/e2e_cli_test.rs" \
  --silent)
echo "E2E_VERIFY=$E2E_VERIFY"
bd dep add "$E2E_VERIFY" "$CLI_COLLECTOR_CMDS"
bd dep add "$E2E_VERIFY" "$CLI_STORAGE_CMDS"
bd dep add "$E2E_VERIFY" "$PROCESS_LIFECYCLE"

# ==========================================================================
# Phase 9: Documentation & Cleanup
# ==========================================================================

PARITY_DOC=$(bd create \
  "Create PARITY.md tracking upstream OTel Collector feature coverage" \
  -p 2 --label docs \
  -d "Create PARITY.md documenting implemented OTel Collector features.

Contents:
- Header: upstream repo URL + parity commit hash (fdadab8a8)
- Feature coverage table:
  | Feature | Status | Notes |
  |---------|--------|-------|
  | OTLP gRPC receiver | Implemented | tonic, port 4317 |
  | OTLP HTTP receiver | Implemented | axum, port 4318 |
  | Batch processor | Implemented | configurable timeout/size |
  | File exporter (JSONL) | Implemented | append mode |
  | Health check extension | Implemented | HTTP 200/503 on 13133 |
  | Memory limiter | Not implemented | Not needed for local dev |
  | Debug exporter | Not implemented | Out of scope |
  | Other receivers | Not implemented | Out of scope |
  | Other exporters | Not implemented | Out of scope |
- opentelemetry-proto crate version mapping
- Instructions for checking upstream changes
- Decision log: why features were excluded (local dev scope)

File reservations: PARITY.md" \
  --silent)
echo "PARITY_DOC=$PARITY_DOC"
bd dep add "$PARITY_DOC" "$LIBRARY_API"

UPDATE_README=$(bd create \
  "Update README.md for Rust binary with new build and usage instructions" \
  -p 2 --label docs \
  -d "Update README.md to reflect the Rust rewrite.

Changes:
- Requirements: Rust toolchain (no Docker needed)
- Build: cargo build --release
- Binary location: target/release/lotel-cli
- Architecture diagram: remove Docker, show native collector
- Quick Start: cargo build, then ./target/release/lotel-cli start --wait
- Add Library Usage section showing Cargo.toml dependency and Rust example
- Keep all command documentation identical (CLI surface unchanged)
- Update Data Storage section (same paths, no container state)
- Keep verify.py documentation

File reservations: README.md" \
  --silent)
echo "UPDATE_README=$UPDATE_README"
bd dep add "$UPDATE_README" "$E2E_VERIFY"

UPDATE_AGENTS=$(bd create \
  "Update AGENTS.md for Rust development workflow" \
  -p 2 --label docs \
  -d "Update AGENTS.md to reflect the Rust codebase.

Changes:
- Build command: cargo build --release
- Binary: ./target/release/lotel-cli (instead of ./lotel)
- Quality gates: cargo test --workspace, cargo build --workspace, cargo clippy --workspace, cargo fmt --check
- Key paths:
  - crates/lotel-cli/src/main.rs - CLI entrypoint (clap)
  - crates/lotel-collector/src/ - native collector
  - crates/lotel-collector/src/config.rs - config and defaults
  - crates/lotel-storage/src/ - DuckDB storage layer
  - scripts/verify.py - E2E verification
- Standard operating loop: cargo build --release first
- Landing: cargo test && cargo build && cargo clippy

File reservations: AGENTS.md" \
  --silent)
echo "UPDATE_AGENTS=$UPDATE_AGENTS"
bd dep add "$UPDATE_AGENTS" "$E2E_VERIFY"

CLEANUP_GO=$(bd create \
  "Remove Go source code after Rust binary passes all tests" \
  -p 3 --label cleanup \
  -d "Once Rust binary passes all E2E tests (verify.py), remove Go code.

Steps:
1. Create a git tag: git tag go-final (preserve Go version reference)
2. Remove:
   - cmd/ directory
   - internal/ directory
   - go.mod, go.sum
3. Keep:
   - scripts/verify.py
   - docs/
   - .beads/
   - AGENTS.md (already updated)
   - README.md (already updated)
   - PARITY.md
   - All Rust code
4. Verify: cargo build --workspace && cargo test --workspace still pass
5. Verify: verify.py still passes with Rust binary

This is the LAST task. Only execute after full E2E verification.

File reservations: cmd/, internal/, go.mod, go.sum" \
  --silent)
echo "CLEANUP_GO=$CLEANUP_GO"
bd dep add "$CLEANUP_GO" "$E2E_VERIFY"
bd dep add "$CLEANUP_GO" "$UPDATE_README"
bd dep add "$CLEANUP_GO" "$UPDATE_AGENTS"

echo ""
echo "========================================"
echo "Bead graph created successfully!"
echo "========================================"
echo ""
echo "Summary:"
echo "  Phase 1 - Setup:           3 beads (SETUP_WORKSPACE, SETUP_CI, SETUP_PROTO)"
echo "  Phase 2 - Core Model:      2 beads (DATA_MODEL, CONFIG_TYPES)"
echo "  Phase 3 - Collector:       5 beads (RECEIVER_GRPC, RECEIVER_HTTP, BATCH_PROCESSOR, FILE_EXPORTER, HEALTH_CHECK)"
echo "  Phase 4 - Pipeline/API:    2 beads (PIPELINE, LIBRARY_API)"
echo "  Phase 5 - Storage:         7 beads (DB_SCHEMA, INGEST x3, QUERY, PRUNE, STORAGE_LIB)"
echo "  Phase 6 - CLI:             3 beads (CLI_SCAFFOLD, CLI_COLLECTOR_CMDS, CLI_STORAGE_CMDS)"
echo "  Phase 7 - Process:         1 bead  (PROCESS_LIFECYCLE)"
echo "  Phase 8 - Testing:         4 beads (UNIT_TESTS x2, INTEGRATION, E2E_VERIFY)"
echo "  Phase 9 - Docs/Cleanup:    4 beads (PARITY_DOC, README, AGENTS, CLEANUP_GO)"
echo "  Total:                    31 beads"
echo ""
echo "View the graph:"
echo "  bd list                    # List all beads"
echo "  bd ready                   # Show unblocked tasks"
echo ""
echo "Parallel execution tracks:"
echo "  Track A: Proto → Data model → Receivers → Pipeline → Library API"
echo "  Track B: Config types → Receivers, File exporter, Health check"
echo "  Track C: DB schema → Ingest (3x) → Query → Prune → Storage lib"
echo "  Track D: CLI scaffold → CLI commands"
echo ""
echo "Initial ready tasks (no blockers):"
echo "  $SETUP_WORKSPACE - Initialize Rust workspace"
