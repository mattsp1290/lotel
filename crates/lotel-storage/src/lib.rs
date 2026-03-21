//! lotel-storage: DuckDB-backed storage for telemetry data.

pub mod db;
pub mod ingest;
pub mod ingest_incremental;
pub mod prune;
pub mod query;

// Re-export key types and functions at crate root.
pub use db::{default_db, open_db, open_in_memory};
pub use ingest::ingest_all;
pub use ingest_incremental::{IncrementalIngester, IngestReport};
pub use prune::{PruneReport, prune};
pub use query::{
    LogResult, MetricAggregation, MetricResult, QueryOptions, TraceResult, aggregate_metrics,
    query_logs, query_metrics, query_traces,
};
