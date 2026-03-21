//! lotel-collector: OTLP collector for receiving and forwarding telemetry data.

pub mod config;
pub mod exporter;
pub mod extension;
pub mod model;
pub mod pipeline;
pub mod processor;
pub mod receiver;

#[cfg(test)]
mod proto_check;

use std::path::Path;
use std::time::Duration;

use config::{CollectorConfig, ConfigError};
use pipeline::{Pipeline, PipelineHandle};

/// High-level collector interface.
pub struct Collector {
    config: CollectorConfig,
}

/// Handle to a running collector for lifecycle management.
pub struct CollectorHandle {
    pipeline: PipelineHandle,
    health_endpoint: String,
    start_time: std::time::Instant,
    config: CollectorConfig,
}

/// Status of the collector.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct CollectorStatus {
    pub running: bool,
    pub healthy: bool,
    pub uptime_secs: f64,
}

impl Collector {
    /// Create from an existing config.
    pub fn new(config: CollectorConfig) -> Self {
        Self { config }
    }

    /// Load config from a file path.
    pub fn from_config_file(path: &Path) -> Result<Self, ConfigError> {
        let content = std::fs::read_to_string(path).map_err(|e| ConfigError::ReadFile {
            path: path.to_path_buf(),
            source: e,
        })?;
        let config = config::parse_config(&content)?;
        Ok(Self { config })
    }

    /// Create with default configuration.
    pub fn with_defaults() -> Result<Self, ConfigError> {
        let config = config::parse_config(config::DEFAULT_CONFIG)?;
        Ok(Self { config })
    }

    /// Start the collector pipeline.
    pub fn start(self) -> Result<CollectorHandle, Box<dyn std::error::Error>> {
        let health_endpoint = format!(
            "http://{}",
            self.config.extensions.health_check.endpoint
        );
        let pipeline = Pipeline::run(&self.config)?;
        Ok(CollectorHandle {
            pipeline,
            health_endpoint,
            start_time: std::time::Instant::now(),
            config: self.config,
        })
    }
}

impl CollectorHandle {
    /// Gracefully shut down the collector.
    pub async fn shutdown(self) {
        self.pipeline.shutdown().await;
    }

    /// Poll health endpoint until ready or timeout.
    pub async fn wait_healthy(&self, timeout: Duration) -> Result<(), Box<dyn std::error::Error>> {
        let start = std::time::Instant::now();
        let client = reqwest::Client::new();
        loop {
            if start.elapsed() > timeout {
                return Err("collector did not become healthy within timeout".into());
            }
            match client.get(&self.health_endpoint).send().await {
                Ok(resp) if resp.status().is_success() => return Ok(()),
                _ => {}
            }
            tokio::time::sleep(Duration::from_millis(500)).await;
        }
    }

    /// Check if the collector is currently healthy.
    pub async fn is_healthy(&self) -> bool {
        let client = reqwest::Client::new();
        matches!(
            client.get(&self.health_endpoint).send().await,
            Ok(resp) if resp.status().is_success()
        )
    }

    /// Get current collector status.
    pub async fn status(&self) -> CollectorStatus {
        CollectorStatus {
            running: true,
            healthy: self.is_healthy().await,
            uptime_secs: self.start_time.elapsed().as_secs_f64(),
        }
    }

    /// Access the underlying config.
    pub fn config(&self) -> &CollectorConfig {
        &self.config
    }
}
