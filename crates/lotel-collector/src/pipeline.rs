//! Collector pipeline types and orchestration.

use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use opentelemetry_proto::tonic::collector::logs::v1::ExportLogsServiceRequest;
use opentelemetry_proto::tonic::collector::metrics::v1::ExportMetricsServiceRequest;
use opentelemetry_proto::tonic::collector::trace::v1::ExportTraceServiceRequest;
use tokio::sync::mpsc;
use tokio::task::JoinHandle;
use tokio_util::sync::CancellationToken;

use crate::config::CollectorConfig;
use crate::exporter::file::FileExporter;
use crate::extension::health::HealthCheckExtension;
use crate::processor::batch::BatchProcessor;
use crate::receiver::grpc::OtlpGrpcReceiver;
use crate::receiver::http::OtlpHttpReceiver;

/// Data flowing through the collector pipeline.
#[derive(Debug, Clone)]
pub enum SignalData {
    Traces(ExportTraceServiceRequest),
    Metrics(ExportMetricsServiceRequest),
    Logs(ExportLogsServiceRequest),
}

/// Handle to a running pipeline for coordinated shutdown.
pub struct PipelineHandle {
    cancel: CancellationToken,
    handles: Vec<JoinHandle<()>>,
}

impl PipelineHandle {
    /// Gracefully shut down all pipeline components.
    pub async fn shutdown(self) {
        self.cancel.cancel();
        for handle in self.handles {
            let _ = handle.await;
        }
    }
}

/// Build and run the full collector pipeline from config.
pub struct Pipeline;

impl Pipeline {
    pub fn run(config: &CollectorConfig) -> Result<PipelineHandle, Box<dyn std::error::Error>> {
        let cancel = CancellationToken::new();
        let ready = Arc::new(AtomicBool::new(false));

        // Parse endpoints from config.
        let grpc_addr: SocketAddr = config.receivers.otlp.protocols.grpc.endpoint.parse()?;
        let http_addr: SocketAddr = config.receivers.otlp.protocols.http.endpoint.parse()?;
        let health_addr: SocketAddr = config.extensions.health_check.endpoint.parse()?;

        // Parse batch config.
        let batch_timeout = parse_batch_timeout(&config.processors.batch.timeout);
        let batch_size = config.processors.batch.send_batch_size;
        let batch_max = config.processors.batch.send_batch_max_size;

        // Resolve exporter paths from config.
        let home = dirs::home_dir().unwrap_or_else(|| PathBuf::from("."));
        let resolve_path = |path: &str| -> PathBuf {
            if path.starts_with("~/") {
                home.join(&path[2..])
            } else {
                PathBuf::from(path)
            }
        };

        let traces_path = config
            .exporters
            .get("file/traces")
            .map(|e| resolve_path(&e.path))
            .unwrap_or_else(|| home.join(".lotel/data/traces/traces.jsonl"));
        let metrics_path = config
            .exporters
            .get("file/metrics")
            .map(|e| resolve_path(&e.path))
            .unwrap_or_else(|| home.join(".lotel/data/metrics/metrics.jsonl"));
        let logs_path = config
            .exporters
            .get("file/logs")
            .map(|e| resolve_path(&e.path))
            .unwrap_or_else(|| home.join(".lotel/data/logs/logs.jsonl"));

        // Create channels: receivers -> processor -> exporter.
        let (recv_tx, recv_rx) = mpsc::channel::<SignalData>(4096);
        let (proc_tx, proc_rx) = mpsc::channel::<SignalData>(4096);

        let mut handles = Vec::new();

        // Spawn health check.
        let health_ext = HealthCheckExtension {
            endpoint: health_addr,
            ready: ready.clone(),
        };
        let health_cancel = cancel.clone();
        handles.push(tokio::spawn(async move {
            if let Err(e) = health_ext.run(health_cancel).await {
                tracing::error!("health check error: {e}");
            }
        }));

        // Spawn gRPC receiver.
        let grpc_receiver = OtlpGrpcReceiver::new(grpc_addr, recv_tx.clone());
        let grpc_cancel = cancel.clone();
        handles.push(tokio::spawn(async move {
            if let Err(e) = grpc_receiver.serve(grpc_cancel).await {
                tracing::error!("gRPC receiver error: {e}");
            }
        }));

        // Spawn HTTP receiver.
        let http_receiver = OtlpHttpReceiver::new(http_addr, recv_tx);
        let http_cancel = cancel.clone();
        handles.push(tokio::spawn(async move {
            if let Err(e) = http_receiver.serve(http_cancel).await {
                tracing::error!("HTTP receiver error: {e}");
            }
        }));

        // Spawn batch processor.
        let processor = BatchProcessor {
            timeout: batch_timeout,
            send_batch_size: batch_size,
            send_batch_max_size: batch_max,
        };
        let proc_cancel = cancel.clone();
        handles.push(tokio::spawn(async move {
            if let Err(e) = processor.run(recv_rx, proc_tx, proc_cancel).await {
                tracing::error!("batch processor error: {e}");
            }
        }));

        // Spawn file exporter.
        let exporter = FileExporter {
            traces_path,
            metrics_path,
            logs_path,
        };
        let exp_cancel = cancel.clone();
        handles.push(tokio::spawn(async move {
            if let Err(e) = exporter.run(proc_rx, exp_cancel).await {
                tracing::error!("file exporter error: {e}");
            }
        }));

        // Mark as ready.
        ready.store(true, Ordering::Relaxed);

        Ok(PipelineHandle { cancel, handles })
    }
}

fn parse_batch_timeout(s: &str) -> Duration {
    // Support simple formats like "1s", "500ms".
    if let Some(secs) = s.strip_suffix('s') {
        if let Ok(n) = secs.parse::<f64>() {
            return Duration::from_secs_f64(n);
        }
    }
    if let Some(ms) = s.strip_suffix("ms") {
        if let Ok(n) = ms.parse::<u64>() {
            return Duration::from_millis(n);
        }
    }
    Duration::from_secs(1)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_batch_timeout_seconds() {
        assert_eq!(parse_batch_timeout("1s"), Duration::from_secs(1));
        assert_eq!(parse_batch_timeout("5s"), Duration::from_secs(5));
    }

    #[test]
    fn parse_batch_timeout_millis() {
        assert_eq!(parse_batch_timeout("500ms"), Duration::from_millis(500));
    }

    #[test]
    fn parse_batch_timeout_fallback() {
        assert_eq!(parse_batch_timeout("invalid"), Duration::from_secs(1));
    }
}
