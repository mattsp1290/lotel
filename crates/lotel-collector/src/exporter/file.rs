use std::fs::{self, OpenOptions};
use std::io::{BufWriter, Write};
use std::path::PathBuf;

use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;

use crate::pipeline::SignalData;

/// File exporter that writes OTLP data as JSONL (one JSON object per line).
pub struct FileExporter {
    pub traces_path: PathBuf,
    pub metrics_path: PathBuf,
    pub logs_path: PathBuf,
}

impl FileExporter {
    /// Run the exporter, consuming from the channel until cancelled.
    pub async fn run(
        self,
        mut rx: mpsc::Receiver<SignalData>,
        cancel: CancellationToken,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        loop {
            tokio::select! {
                _ = cancel.cancelled() => {
                    // Drain remaining items.
                    while let Ok(data) = rx.try_recv() {
                        self.write_signal(&data)?;
                    }
                    break;
                }
                msg = rx.recv() => {
                    match msg {
                        Some(data) => self.write_signal(&data)?,
                        None => break,
                    }
                }
            }
        }
        Ok(())
    }

    fn write_signal(
        &self,
        data: &SignalData,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        match data {
            SignalData::Traces(req) => self.append_json(&self.traces_path, req),
            SignalData::Metrics(req) => self.append_json(&self.metrics_path, req),
            SignalData::Logs(req) => self.append_json(&self.logs_path, req),
        }
    }

    fn append_json<T: serde::Serialize>(
        &self,
        path: &PathBuf,
        value: &T,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        let file = OpenOptions::new().create(true).append(true).open(path)?;
        let mut writer = BufWriter::new(file);
        serde_json::to_writer(&mut writer, value)?;
        writeln!(writer)?;
        writer.flush()?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry_proto::tonic::collector::trace::v1::ExportTraceServiceRequest;
    use opentelemetry_proto::tonic::trace::v1::{ResourceSpans, ScopeSpans, Span};
    use std::io::BufRead;
    use tempfile::TempDir;

    #[tokio::test]
    async fn writes_valid_jsonl() {
        let tmp = TempDir::new().unwrap();
        let traces_path = tmp.path().join("traces/traces.jsonl");
        let metrics_path = tmp.path().join("metrics/metrics.jsonl");
        let logs_path = tmp.path().join("logs/logs.jsonl");

        let (tx, rx) = mpsc::channel(16);
        let cancel = CancellationToken::new();

        let exporter = FileExporter {
            traces_path: traces_path.clone(),
            metrics_path,
            logs_path,
        };

        let cancel_clone = cancel.clone();
        let handle = tokio::spawn(async move {
            exporter.run(rx, cancel_clone).await.unwrap();
        });

        // Send two trace batches.
        let req = ExportTraceServiceRequest {
            resource_spans: vec![ResourceSpans {
                scope_spans: vec![ScopeSpans {
                    spans: vec![Span {
                        name: "test-span".into(),
                        ..Default::default()
                    }],
                    ..Default::default()
                }],
                ..Default::default()
            }],
        };

        tx.send(SignalData::Traces(req.clone())).await.unwrap();
        tx.send(SignalData::Traces(req)).await.unwrap();

        // Small delay to let writes complete.
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;
        cancel.cancel();
        handle.await.unwrap();

        // Read back and verify.
        let file = fs::File::open(&traces_path).unwrap();
        let reader = std::io::BufReader::new(file);
        let lines: Vec<String> = reader.lines().map(|l| l.unwrap()).collect();
        assert_eq!(lines.len(), 2, "should have 2 JSONL lines");

        // Each line should be valid JSON.
        for line in &lines {
            let parsed: serde_json::Value = serde_json::from_str(line).unwrap();
            // Proto serde may use either snake_case or camelCase.
            assert!(
                parsed.get("resource_spans").is_some() || parsed.get("resourceSpans").is_some(),
                "expected resource_spans or resourceSpans in: {line}"
            );
        }
    }
}
