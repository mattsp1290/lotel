//! Incremental ingestion that tracks file byte offsets to avoid duplicates.

use std::collections::HashMap;
use std::io::{BufRead, BufReader, Seek, SeekFrom};
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use duckdb::Connection;

use crate::ingest::{ingest_log_line, ingest_metric_line, ingest_trace_line};

/// Report of how many records were ingested in a single run.
#[derive(Debug, Default)]
pub struct IngestReport {
    pub traces: usize,
    pub metrics: usize,
    pub logs: usize,
}

impl IngestReport {
    pub fn total(&self) -> usize {
        self.traces + self.metrics + self.logs
    }
}

impl std::fmt::Display for IngestReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{} traces, {} metrics, {} logs",
            self.traces, self.metrics, self.logs
        )
    }
}

type IngestLineFn = fn(&duckdb::Transaction<'_>, &str) -> Result<usize>;

/// Tracks byte offsets per JSONL file to only ingest new data.
#[derive(Default)]
pub struct IncrementalIngester {
    offsets: HashMap<PathBuf, u64>,
}

impl IncrementalIngester {
    pub fn new() -> Self {
        Self::default()
    }

    /// Ingest new data from all three signal files starting from tracked offsets.
    pub fn ingest_new(&mut self, conn: &Connection, data_path: &Path) -> Result<IngestReport> {
        let mut report = IngestReport::default();

        let signals: [(&str, IngestLineFn); 3] = [
            ("traces", ingest_trace_line as IngestLineFn),
            ("metrics", ingest_metric_line as IngestLineFn),
            ("logs", ingest_log_line as IngestLineFn),
        ];

        for (signal, ingest_fn) in &signals {
            let file_path = data_path.join(signal).join(format!("{signal}.jsonl"));
            if !file_path.exists() {
                continue;
            }

            let metadata = std::fs::metadata(&file_path)
                .with_context(|| format!("reading metadata for {signal}"))?;
            let file_size = metadata.len();
            let offset = self.offsets.get(&file_path).copied().unwrap_or(0);

            if file_size <= offset {
                continue;
            }

            let ingested = self.ingest_file(conn, &file_path, offset, *ingest_fn)?;
            match *signal {
                "traces" => report.traces = ingested,
                "metrics" => report.metrics = ingested,
                "logs" => report.logs = ingested,
                _ => {}
            }
        }

        Ok(report)
    }

    fn ingest_file(
        &mut self,
        conn: &Connection,
        file_path: &Path,
        offset: u64,
        ingest_fn: IngestLineFn,
    ) -> Result<usize> {
        let mut file = std::fs::File::open(file_path)?;
        file.seek(SeekFrom::Start(offset))?;
        let mut reader = BufReader::new(file);

        let tx = conn.unchecked_transaction()?;
        let mut total_count = 0;
        let mut new_offset = offset;
        let mut line = String::new();

        loop {
            line.clear();
            let bytes_read = reader.read_line(&mut line)?;
            if bytes_read == 0 {
                break;
            }
            new_offset += bytes_read as u64;

            let trimmed = line.trim();
            if trimmed.is_empty() {
                continue;
            }
            total_count += ingest_fn(&tx, trimmed)?;
        }

        tx.commit()?;
        self.offsets.insert(file_path.to_path_buf(), new_offset);
        Ok(total_count)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db;

    #[test]
    fn incremental_ingest_no_duplicates() {
        let conn = db::open_in_memory().unwrap();
        let tmp = tempfile::TempDir::new().unwrap();
        let traces_dir = tmp.path().join("traces");
        std::fs::create_dir_all(&traces_dir).unwrap();
        let file = traces_dir.join("traces.jsonl");

        let line1 = r#"{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc-a"}}]},"scopeSpans":[{"spans":[{"traceId":"aaa","spanId":"111","name":"span-1","kind":1,"startTimeUnixNano":"1710000000000000000","endTimeUnixNano":"1710000001000000000","status":{"code":0},"attributes":[]}]}]}]}"#;
        std::fs::write(&file, format!("{line1}\n")).unwrap();

        let mut ingester = IncrementalIngester::new();

        // First ingest: should get 1 trace.
        let report = ingester.ingest_new(&conn, tmp.path()).unwrap();
        assert_eq!(report.traces, 1);

        // Second ingest with no new data: should get 0.
        let report = ingester.ingest_new(&conn, tmp.path()).unwrap();
        assert_eq!(report.traces, 0);

        // Verify only 1 row in DB (no duplicates).
        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM traces", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 1);
    }

    #[test]
    fn incremental_ingest_picks_up_appended_data() {
        let conn = db::open_in_memory().unwrap();
        let tmp = tempfile::TempDir::new().unwrap();
        let traces_dir = tmp.path().join("traces");
        std::fs::create_dir_all(&traces_dir).unwrap();
        let file = traces_dir.join("traces.jsonl");

        let line1 = r#"{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc-a"}}]},"scopeSpans":[{"spans":[{"traceId":"aaa","spanId":"111","name":"span-1","kind":1,"startTimeUnixNano":"1710000000000000000","endTimeUnixNano":"1710000001000000000","status":{"code":0},"attributes":[]}]}]}]}"#;
        std::fs::write(&file, format!("{line1}\n")).unwrap();

        let mut ingester = IncrementalIngester::new();
        ingester.ingest_new(&conn, tmp.path()).unwrap();

        // Append new data.
        let line2 = r#"{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc-a"}}]},"scopeSpans":[{"spans":[{"traceId":"bbb","spanId":"222","name":"span-2","kind":1,"startTimeUnixNano":"1710000002000000000","endTimeUnixNano":"1710000003000000000","status":{"code":0},"attributes":[]}]}]}]}"#;
        use std::io::Write;
        let mut f = std::fs::OpenOptions::new()
            .append(true)
            .open(&file)
            .unwrap();
        writeln!(f, "{line2}").unwrap();

        // Second ingest: should pick up only the new line.
        let report = ingester.ingest_new(&conn, tmp.path()).unwrap();
        assert_eq!(report.traces, 1);

        // Total should be 2 (no duplicates).
        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM traces", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 2);
    }

    #[test]
    fn incremental_ingest_handles_missing_files() {
        let conn = db::open_in_memory().unwrap();
        let tmp = tempfile::TempDir::new().unwrap();
        let mut ingester = IncrementalIngester::new();
        // No files exist — should not error.
        let report = ingester.ingest_new(&conn, tmp.path()).unwrap();
        assert_eq!(report.total(), 0);
    }
}
