use std::net::SocketAddr;

use axum::extract::State;
use axum::http::StatusCode;
use axum::routing::post;
use axum::Json;
use opentelemetry_proto::tonic::collector::logs::v1::ExportLogsServiceRequest;
use opentelemetry_proto::tonic::collector::metrics::v1::ExportMetricsServiceRequest;
use opentelemetry_proto::tonic::collector::trace::v1::ExportTraceServiceRequest;
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;

use crate::pipeline::SignalData;

/// OTLP HTTP receiver that forwards data through a channel.
pub struct OtlpHttpReceiver {
    endpoint: SocketAddr,
    tx: mpsc::Sender<SignalData>,
}

#[derive(Clone)]
struct AppState {
    tx: mpsc::Sender<SignalData>,
}

impl OtlpHttpReceiver {
    pub fn new(endpoint: SocketAddr, tx: mpsc::Sender<SignalData>) -> Self {
        Self { endpoint, tx }
    }

    pub async fn serve(self, cancel: CancellationToken) -> Result<(), Box<dyn std::error::Error>> {
        let state = AppState { tx: self.tx };

        let app = axum::Router::new()
            .route("/v1/traces", post(handle_traces))
            .route("/v1/metrics", post(handle_metrics))
            .route("/v1/logs", post(handle_logs))
            .with_state(state);

        let listener = tokio::net::TcpListener::bind(self.endpoint).await?;
        axum::serve(listener, app)
            .with_graceful_shutdown(cancel.cancelled_owned())
            .await?;

        Ok(())
    }
}

async fn handle_traces(
    State(state): State<AppState>,
    Json(request): Json<ExportTraceServiceRequest>,
) -> StatusCode {
    match state.tx.send(SignalData::Traces(request)).await {
        Ok(()) => StatusCode::OK,
        Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

async fn handle_metrics(
    State(state): State<AppState>,
    Json(request): Json<ExportMetricsServiceRequest>,
) -> StatusCode {
    match state.tx.send(SignalData::Metrics(request)).await {
        Ok(()) => StatusCode::OK,
        Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

async fn handle_logs(
    State(state): State<AppState>,
    Json(request): Json<ExportLogsServiceRequest>,
) -> StatusCode {
    match state.tx.send(SignalData::Logs(request)).await {
        Ok(()) => StatusCode::OK,
        Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn http_receiver_forwards_traces() {
        let (tx, mut rx) = mpsc::channel(16);
        let cancel = CancellationToken::new();

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        drop(listener);

        let receiver = OtlpHttpReceiver::new(addr, tx);
        let cancel_clone = cancel.clone();
        let server_handle = tokio::spawn(async move {
            receiver.serve(cancel_clone).await.unwrap();
        });

        tokio::time::sleep(std::time::Duration::from_millis(100)).await;

        let client = reqwest::Client::new();
        // Build request using the actual proto types so serde format matches exactly.
        use opentelemetry_proto::tonic::trace::v1::{ResourceSpans, ScopeSpans, Span};
        let trace_req = ExportTraceServiceRequest {
            resource_spans: vec![ResourceSpans {
                scope_spans: vec![ScopeSpans {
                    spans: vec![Span {
                        name: "http-test-span".into(),
                        ..Default::default()
                    }],
                    ..Default::default()
                }],
                ..Default::default()
            }],
        };
        let trace_data = serde_json::to_value(&trace_req).unwrap();

        let resp = client
            .post(format!("http://{addr}/v1/traces"))
            .json(&trace_data)
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 200);

        let received = rx.recv().await.unwrap();
        match received {
            SignalData::Traces(req) => {
                assert_eq!(req.resource_spans.len(), 1);
            }
            _ => panic!("expected Traces signal"),
        }

        cancel.cancel();
        server_handle.await.unwrap();
    }
}
