use std::time::Duration;

use tokio::sync::mpsc;
use tokio::time;
use tokio_util::sync::CancellationToken;

use crate::pipeline::SignalData;

/// Batch processor that accumulates signal data and flushes periodically
/// or when batch size limits are reached.
pub struct BatchProcessor {
    pub timeout: Duration,
    pub send_batch_size: usize,
    pub send_batch_max_size: usize,
}

impl Default for BatchProcessor {
    fn default() -> Self {
        Self {
            timeout: Duration::from_secs(1),
            send_batch_size: 1024,
            send_batch_max_size: 2048,
        }
    }
}

impl BatchProcessor {
    pub async fn run(
        self,
        mut rx: mpsc::Receiver<SignalData>,
        tx: mpsc::Sender<SignalData>,
        cancel: CancellationToken,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let mut buffer: Vec<SignalData> = Vec::new();
        let mut interval = time::interval(self.timeout);
        // Skip the first immediate tick.
        interval.tick().await;

        loop {
            tokio::select! {
                _ = cancel.cancelled() => {
                    // Flush remaining items on shutdown.
                    flush(&mut buffer, &tx).await;
                    break;
                }
                _ = interval.tick() => {
                    // Timeout flush.
                    flush(&mut buffer, &tx).await;
                }
                msg = rx.recv() => {
                    match msg {
                        Some(data) => {
                            buffer.push(data);
                            // Flush if reaching batch size threshold.
                            if buffer.len() >= self.send_batch_size {
                                flush(&mut buffer, &tx).await;
                            }
                        }
                        None => {
                            // Channel closed, flush and exit.
                            flush(&mut buffer, &tx).await;
                            break;
                        }
                    }
                }
            }
        }

        Ok(())
    }
}

async fn flush(buffer: &mut Vec<SignalData>, tx: &mpsc::Sender<SignalData>) {
    for item in buffer.drain(..) {
        let _ = tx.send(item).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry_proto::tonic::collector::trace::v1::ExportTraceServiceRequest;

    fn test_trace() -> SignalData {
        SignalData::Traces(ExportTraceServiceRequest::default())
    }

    #[tokio::test]
    async fn flush_on_batch_size() {
        let (in_tx, in_rx) = mpsc::channel(100);
        let (out_tx, mut out_rx) = mpsc::channel(100);
        let cancel = CancellationToken::new();

        let processor = BatchProcessor {
            timeout: Duration::from_secs(60), // Long timeout — won't trigger.
            send_batch_size: 3,
            send_batch_max_size: 10,
        };

        let cancel_clone = cancel.clone();
        let handle = tokio::spawn(async move {
            processor.run(in_rx, out_tx, cancel_clone).await.unwrap();
        });

        // Send exactly send_batch_size items.
        for _ in 0..3 {
            in_tx.send(test_trace()).await.unwrap();
        }

        // Should receive all 3.
        for _ in 0..3 {
            let item = time::timeout(Duration::from_secs(2), out_rx.recv())
                .await
                .expect("should receive within timeout")
                .expect("channel should not be closed");
            assert!(matches!(item, SignalData::Traces(_)));
        }

        cancel.cancel();
        handle.await.unwrap();
    }

    #[tokio::test]
    async fn flush_on_timeout() {
        let (in_tx, in_rx) = mpsc::channel(100);
        let (out_tx, mut out_rx) = mpsc::channel(100);
        let cancel = CancellationToken::new();

        let processor = BatchProcessor {
            timeout: Duration::from_millis(100),
            send_batch_size: 1000, // Won't reach this.
            send_batch_max_size: 2000,
        };

        let cancel_clone = cancel.clone();
        let handle = tokio::spawn(async move {
            processor.run(in_rx, out_tx, cancel_clone).await.unwrap();
        });

        // Send fewer items than batch size.
        in_tx.send(test_trace()).await.unwrap();
        in_tx.send(test_trace()).await.unwrap();

        // Wait for timeout flush.
        for _ in 0..2 {
            let item = time::timeout(Duration::from_secs(2), out_rx.recv())
                .await
                .expect("should receive within timeout")
                .expect("channel should not be closed");
            assert!(matches!(item, SignalData::Traces(_)));
        }

        cancel.cancel();
        handle.await.unwrap();
    }

    #[tokio::test]
    async fn shutdown_flushes_remaining() {
        let (in_tx, in_rx) = mpsc::channel(100);
        let (out_tx, mut out_rx) = mpsc::channel(100);
        let cancel = CancellationToken::new();

        let processor = BatchProcessor {
            timeout: Duration::from_secs(60),
            send_batch_size: 1000,
            send_batch_max_size: 2000,
        };

        let cancel_clone = cancel.clone();
        let handle = tokio::spawn(async move {
            processor.run(in_rx, out_tx, cancel_clone).await.unwrap();
        });

        // Send items without reaching batch size.
        in_tx.send(test_trace()).await.unwrap();

        // Small delay to ensure the item is buffered.
        time::sleep(Duration::from_millis(50)).await;

        // Cancel — should flush remaining items.
        cancel.cancel();
        handle.await.unwrap();

        let item = out_rx.recv().await.expect("should receive flushed item");
        assert!(matches!(item, SignalData::Traces(_)));
    }
}
