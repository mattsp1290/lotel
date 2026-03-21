use std::net::SocketAddr;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};

use axum::extract::State;
use axum::http::StatusCode;
use axum::routing::get;
use tokio_util::sync::CancellationToken;

/// Health check HTTP extension serving readiness status.
pub struct HealthCheckExtension {
    pub endpoint: SocketAddr,
    pub ready: Arc<AtomicBool>,
}

impl HealthCheckExtension {
    pub async fn run(
        self,
        cancel: CancellationToken,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        let app = axum::Router::new()
            .route("/", get(handle_health))
            .with_state(self.ready);

        let listener = tokio::net::TcpListener::bind(self.endpoint).await?;
        axum::serve(listener, app)
            .with_graceful_shutdown(cancel.cancelled_owned())
            .await?;

        Ok(())
    }
}

async fn handle_health(State(ready): State<Arc<AtomicBool>>) -> StatusCode {
    if ready.load(Ordering::Relaxed) {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn returns_503_when_not_ready() {
        let ready = Arc::new(AtomicBool::new(false));
        let cancel = CancellationToken::new();

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        drop(listener);

        let ext = HealthCheckExtension {
            endpoint: addr,
            ready: ready.clone(),
        };

        let cancel_clone = cancel.clone();
        let handle = tokio::spawn(async move {
            ext.run(cancel_clone).await.unwrap();
        });

        tokio::time::sleep(std::time::Duration::from_millis(100)).await;

        let resp = reqwest::get(format!("http://{addr}/")).await.unwrap();
        assert_eq!(resp.status(), 503);

        cancel.cancel();
        handle.await.unwrap();
    }

    #[tokio::test]
    async fn returns_200_when_ready() {
        let ready = Arc::new(AtomicBool::new(true));
        let cancel = CancellationToken::new();

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        drop(listener);

        let ext = HealthCheckExtension {
            endpoint: addr,
            ready: ready.clone(),
        };

        let cancel_clone = cancel.clone();
        let handle = tokio::spawn(async move {
            ext.run(cancel_clone).await.unwrap();
        });

        tokio::time::sleep(std::time::Duration::from_millis(100)).await;

        let resp = reqwest::get(format!("http://{addr}/")).await.unwrap();
        assert_eq!(resp.status(), 200);

        cancel.cancel();
        handle.await.unwrap();
    }
}
