//! lotel-storage: DuckDB-backed storage for telemetry data.

pub mod db;
pub mod ingest;
pub mod prune;
pub mod query;

// Re-export key types and functions at crate root.
pub use db::{default_db, open_db, open_in_memory};
pub use ingest::ingest_all;
pub use prune::{prune, PruneReport};
pub use query::{
    aggregate_metrics, query_logs, query_metrics, query_traces, LogResult, MetricAggregation,
    MetricResult, QueryOptions, TraceResult,
};
