# Rust Rewrite Parity Tracker

Tracking parity between the Go implementation and the Rust rewrite.

**Upstream reference**: Go implementation at commit `fdadab8a8` (pre-rewrite baseline).

## Component Status

| Component | Go Source | Rust Crate | Status |
|-----------|-----------|------------|--------|
| Config/YAML parsing | `internal/config/config.go` | `lotel-collector::config` | Done |
| DuckDB schema/migration | `internal/storage/db.go` | `lotel-storage::db` | Done |
| CLI scaffold + commands | `cmd/lotel/main.go` | `lotel-cli` | Done |
| OTLP proto types | (otlp libs) | `opentelemetry-proto` v0.31 | Validated |
| OTLP gRPC receiver | `internal/collector/` | `lotel-collector::receiver::grpc` | Done (tonic) |
| OTLP HTTP receiver | `internal/collector/` | `lotel-collector::receiver::http` | Done (axum) |
| Batch processor | - | `lotel-collector::processor::batch` | Done |
| JSONL file exporter | `internal/collector/` | `lotel-collector::exporter::file` | Done |
| Health check extension | `internal/collector/` | `lotel-collector::extension::health` | Done |
| Pipeline orchestration | `internal/collector/` | `lotel-collector::pipeline` | Done |
| Public collector API | - | `lotel-collector::{Collector,CollectorHandle}` | Done |
| Internal data model | `internal/storage/ingest.go` | `lotel-collector::model` | Done |
| Traces ingestion | `internal/storage/ingest.go` | `lotel-storage::ingest` | Done |
| Metrics ingestion | `internal/storage/ingest.go` | `lotel-storage::ingest` | Done |
| Logs ingestion | `internal/storage/ingest.go` | `lotel-storage::ingest` | Done |
| Query interface | `internal/storage/query.go` | `lotel-storage::query` | Done |
| Metric aggregation | `internal/storage/query.go` | `lotel-storage::query` | Done |
| Data pruning | `internal/storage/prune.go` | `lotel-storage::prune` | Done |
| Collector lifecycle | `internal/collector/` | `lotel-cli::daemon` | Done |
| CI configuration | - | `.github/workflows/rust.yml` | Done |

## Features Not Implemented (Out of Scope)

| Feature | Reason |
|---------|--------|
| Memory limiter processor | Not needed for local dev workloads |
| Debug exporter | Out of scope for local use |
| Other receivers (Jaeger, Zipkin) | Out of scope — OTLP only |
| Other exporters (OTLP, Jaeger) | File exporter covers local dev needs |
| TLS for gRPC/HTTP | Not needed for localhost |
| Load balancing/sharding | Single-host scope |

## Proto Type Validation

The `opentelemetry-proto` crate (v0.31, features: `gen-tonic`, `trace`, `metrics`, `logs`, `with-serde`) provides all required types:

### Traces
- `ExportTraceServiceRequest`, `ResourceSpans`, `ScopeSpans`, `Span`
- `trace_service_server::TraceServiceServer` / `TraceService` trait

### Metrics
- `ExportMetricsServiceRequest`, `ResourceMetrics`, `ScopeMetrics`, `Metric`
- `Sum`, `Gauge`, `Histogram` data point types
- `metrics_service_server::MetricsServiceServer` / `MetricsService` trait

### Logs
- `ExportLogsServiceRequest`, `ResourceLogs`, `ScopeLogs`, `LogRecord`
- `logs_service_server::LogsServiceServer` / `LogsService` trait

### Common
- `AnyValue`, `KeyValue` for attributes
- Serde JSON serialization/deserialization confirmed via `with-serde` feature
