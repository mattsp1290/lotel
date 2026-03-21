# Rust Rewrite Parity Tracker

Tracking parity between the Go implementation and the Rust rewrite.

**Upstream reference**: Go implementation at commit `fdadab8a8` (pre-rewrite baseline).

## Component Status

| Component | Go Source | Rust Crate | Status |
|-----------|-----------|------------|--------|
| Config/YAML parsing | `internal/config/config.go` | `lotel-collector::config` | Done |
| DuckDB schema/migration | `internal/storage/db.go` | `lotel-storage::db` | Done |
| CLI scaffold | `cmd/lotel/main.go` | `lotel-cli` | Done (stubs) |
| OTLP proto types | (go-duckdb + otlp libs) | `opentelemetry-proto` | Validated |
| OTLP gRPC receiver | `internal/collector/` | `lotel-collector` | Not started |
| OTLP HTTP receiver | `internal/collector/` | `lotel-collector` | Not started |
| JSONL file exporter | `internal/collector/` | `lotel-collector` | Not started |
| Health check extension | `internal/collector/` | `lotel-collector` | Not started |
| Traces ingestion | `internal/storage/ingest.go` | `lotel-storage` | Not started |
| Metrics ingestion | `internal/storage/ingest.go` | `lotel-storage` | Not started |
| Logs ingestion | `internal/storage/ingest.go` | `lotel-storage` | Not started |
| Query interface | `internal/storage/query.go` | `lotel-storage` | Not started |
| Data pruning | `internal/storage/prune.go` | `lotel-storage` | Not started |
| Collector lifecycle | `internal/collector/` | `lotel-collector` | Not started |
| CI configuration | - | - | Not started |

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
