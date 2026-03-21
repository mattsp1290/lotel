use std::net::SocketAddr;

use opentelemetry_proto::tonic::collector::logs::v1::{
    ExportLogsServiceRequest, ExportLogsServiceResponse,
    logs_service_server::{LogsService, LogsServiceServer},
};
use opentelemetry_proto::tonic::collector::metrics::v1::{
    ExportMetricsServiceRequest, ExportMetricsServiceResponse,
    metrics_service_server::{MetricsService, MetricsServiceServer},
};
use opentelemetry_proto::tonic::collector::trace::v1::{
    ExportTraceServiceRequest, ExportTraceServiceResponse,
    trace_service_server::{TraceService, TraceServiceServer},
};
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use tonic::{Request, Response, Status};

use crate::pipeline::SignalData;

/// OTLP gRPC receiver that forwards data through a channel.
pub struct OtlpGrpcReceiver {
    endpoint: SocketAddr,
    tx: mpsc::Sender<SignalData>,
}

impl OtlpGrpcReceiver {
    pub fn new(endpoint: SocketAddr, tx: mpsc::Sender<SignalData>) -> Self {
        Self { endpoint, tx }
    }

    pub async fn serve(self, cancel: CancellationToken) -> Result<(), Box<dyn std::error::Error>> {
        let trace_svc = TraceServiceServer::new(TraceHandler {
            tx: self.tx.clone(),
        });
        let metrics_svc = MetricsServiceServer::new(MetricsHandler {
            tx: self.tx.clone(),
        });
        let logs_svc = LogsServiceServer::new(LogsHandler { tx: self.tx });

        let listener = tokio::net::TcpListener::bind(self.endpoint).await?;

        tonic::transport::Server::builder()
            .add_service(trace_svc)
            .add_service(metrics_svc)
            .add_service(logs_svc)
            .serve_with_incoming_shutdown(
                tokio_stream::wrappers::TcpListenerStream::new(listener),
                cancel.cancelled(),
            )
            .await?;

        Ok(())
    }
}

struct TraceHandler {
    tx: mpsc::Sender<SignalData>,
}

#[tonic::async_trait]
impl TraceService for TraceHandler {
    async fn export(
        &self,
        request: Request<ExportTraceServiceRequest>,
    ) -> Result<Response<ExportTraceServiceResponse>, Status> {
        self.tx
            .send(SignalData::Traces(request.into_inner()))
            .await
            .map_err(|_| Status::internal("pipeline channel closed"))?;
        Ok(Response::new(ExportTraceServiceResponse {
            partial_success: None,
        }))
    }
}

struct MetricsHandler {
    tx: mpsc::Sender<SignalData>,
}

#[tonic::async_trait]
impl MetricsService for MetricsHandler {
    async fn export(
        &self,
        request: Request<ExportMetricsServiceRequest>,
    ) -> Result<Response<ExportMetricsServiceResponse>, Status> {
        self.tx
            .send(SignalData::Metrics(request.into_inner()))
            .await
            .map_err(|_| Status::internal("pipeline channel closed"))?;
        Ok(Response::new(ExportMetricsServiceResponse {
            partial_success: None,
        }))
    }
}

struct LogsHandler {
    tx: mpsc::Sender<SignalData>,
}

#[tonic::async_trait]
impl LogsService for LogsHandler {
    async fn export(
        &self,
        request: Request<ExportLogsServiceRequest>,
    ) -> Result<Response<ExportLogsServiceResponse>, Status> {
        self.tx
            .send(SignalData::Logs(request.into_inner()))
            .await
            .map_err(|_| Status::internal("pipeline channel closed"))?;
        Ok(Response::new(ExportLogsServiceResponse {
            partial_success: None,
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry_proto::tonic::collector::trace::v1::trace_service_client::TraceServiceClient;
    use opentelemetry_proto::tonic::trace::v1::{ResourceSpans, ScopeSpans, Span};

    #[tokio::test]
    async fn grpc_receiver_forwards_traces() {
        let (tx, mut rx) = mpsc::channel(16);
        let cancel = CancellationToken::new();

        // Bind to random port.
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        drop(listener);

        let receiver = OtlpGrpcReceiver::new(addr, tx);
        let cancel_clone = cancel.clone();
        let server_handle = tokio::spawn(async move {
            receiver.serve(cancel_clone).await.unwrap();
        });

        // Wait briefly for server to start.
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;

        // Connect client and send a trace request.
        let mut client = TraceServiceClient::connect(format!("http://{addr}"))
            .await
            .unwrap();

        let request = ExportTraceServiceRequest {
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

        client.export(request).await.unwrap();

        // Verify channel received the data.
        let received = rx.recv().await.unwrap();
        match received {
            SignalData::Traces(req) => {
                assert_eq!(req.resource_spans.len(), 1);
                assert_eq!(
                    req.resource_spans[0].scope_spans[0].spans[0].name,
                    "test-span"
                );
            }
            _ => panic!("expected Traces signal"),
        }

        cancel.cancel();
        server_handle.await.unwrap();
    }
}
