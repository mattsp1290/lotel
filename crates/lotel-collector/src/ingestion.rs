//! Periodic ingestion task that runs alongside the collector pipeline.
//!
//! Spawns a dedicated OS thread for DuckDB work (Connection is !Send),
//! and an async ticker that sends signals to the thread on each interval.

use std::path::PathBuf;
use std::time::Duration;

use tokio_util::sync::CancellationToken;

/// Run the periodic ingestion task.
///
/// Opens a DuckDB connection and incrementally ingests new JSONL data
/// on the configured interval. Errors are logged but never crash the collector.
pub async fn run_ingestion_task(
    interval: Duration,
    data_path: PathBuf,
    db_path: PathBuf,
    cancel: CancellationToken,
) {
    let (tx, rx) = std::sync::mpsc::channel::<()>();

    // Spawn a dedicated OS thread for blocking DuckDB work.
    let thread_handle = std::thread::spawn(move || {
        let conn = match lotel_storage::open_db(&db_path) {
            Ok(c) => c,
            Err(e) => {
                tracing::error!("Failed to open DuckDB for ingestion: {e}");
                return;
            }
        };
        let mut ingester = lotel_storage::IncrementalIngester::new();

        // Initial full ingest (reads everything from offset 0).
        match ingester.ingest_new(&conn, &data_path) {
            Ok(report) if report.total() > 0 => {
                tracing::info!("Initial ingestion: {report}");
            }
            Ok(_) => {}
            Err(e) => {
                tracing::error!("Initial ingestion failed: {e}");
            }
        }

        // Wait for ticks from the async side.
        while rx.recv().is_ok() {
            match ingester.ingest_new(&conn, &data_path) {
                Ok(report) if report.total() > 0 => {
                    tracing::info!("Periodic ingestion: {report}");
                }
                Ok(_) => {}
                Err(e) => {
                    tracing::error!("Periodic ingestion failed: {e}");
                }
            }
        }

        tracing::info!("Ingestion thread exiting");
    });

    // Async ticker that sends signals to the blocking thread.
    let mut ticker = tokio::time::interval(interval);
    ticker.tick().await; // Consume the immediate first tick.

    loop {
        tokio::select! {
            _ = cancel.cancelled() => {
                drop(tx);
                break;
            }
            _ = ticker.tick() => {
                if tx.send(()).is_err() {
                    tracing::error!("Ingestion thread died unexpectedly");
                    break;
                }
            }
        }
    }

    // Wait for the thread to finish.
    if let Err(e) = thread_handle.join() {
        tracing::error!("Ingestion thread panicked: {e:?}");
    }
}
