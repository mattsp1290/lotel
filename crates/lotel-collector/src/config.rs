use std::collections::HashMap;
use std::fs;
use std::path::PathBuf;

use serde::Deserialize;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ConfigError {
    #[error("getting home directory")]
    NoHome,
    #[error("creating directory {path}: {source}")]
    CreateDir {
        path: PathBuf,
        source: std::io::Error,
    },
    #[error("writing default config: {0}")]
    WriteDefault(std::io::Error),
    #[error("reading config {path}: {source}")]
    ReadFile {
        path: PathBuf,
        source: std::io::Error,
    },
    #[error("parsing config: {0}")]
    Parse(#[from] serde_yaml::Error),
}

/// Embedded default configuration matching the Go DefaultConfig.
/// Paths use ~/.lotel/data/ instead of /data/ (no Docker volume mapping).
pub const DEFAULT_CONFIG: &str = r#"receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024
    send_batch_max_size: 2048

exporters:
  file/traces:
    path: ~/.lotel/data/traces/traces.jsonl
    format: json
  file/metrics:
    path: ~/.lotel/data/metrics/metrics.jsonl
    format: json
  file/logs:
    path: ~/.lotel/data/logs/logs.jsonl
    format: json

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

ingestion:
  interval: 2m
  enabled: true

service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/traces]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/metrics]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/logs]
  telemetry:
    logs:
      level: info
"#;

const LOTEL_DIR: &str = ".lotel";
const DEFAULT_CONFIG_NAME: &str = "collector-config.yaml";

// --- Config types ---

#[derive(Debug, Deserialize, PartialEq)]
pub struct CollectorConfig {
    pub receivers: Receivers,
    pub processors: Processors,
    pub exporters: HashMap<String, FileExporter>,
    pub extensions: Extensions,
    pub service: Service,
    #[serde(default)]
    pub ingestion: Option<IngestionConfig>,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct IngestionConfig {
    /// How often to run ingestion (e.g., "2m", "5m", "30s").
    #[serde(default = "default_ingestion_interval")]
    pub interval: String,
    /// Enable or disable periodic ingestion.
    #[serde(default = "default_true")]
    pub enabled: bool,
}

fn default_ingestion_interval() -> String {
    "2m".to_string()
}

fn default_true() -> bool {
    true
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Receivers {
    pub otlp: OtlpReceiver,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct OtlpReceiver {
    pub protocols: OtlpProtocols,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct OtlpProtocols {
    pub grpc: Endpoint,
    pub http: Endpoint,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Endpoint {
    pub endpoint: String,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Processors {
    pub batch: BatchProcessor,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct BatchProcessor {
    pub timeout: String,
    pub send_batch_size: usize,
    pub send_batch_max_size: usize,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct FileExporter {
    pub path: String,
    pub format: String,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Extensions {
    pub health_check: Endpoint,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Service {
    pub extensions: Vec<String>,
    pub pipelines: HashMap<String, Pipeline>,
    pub telemetry: Option<Telemetry>,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Pipeline {
    pub receivers: Vec<String>,
    pub processors: Vec<String>,
    pub exporters: Vec<String>,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct Telemetry {
    pub logs: Option<TelemetryLogs>,
}

#[derive(Debug, Deserialize, PartialEq)]
pub struct TelemetryLogs {
    pub level: String,
}

// --- Path resolution ---

fn home_dir() -> Result<PathBuf, ConfigError> {
    dirs::home_dir().ok_or(ConfigError::NoHome)
}

/// Returns the data directory path: ~/.lotel/data/
pub fn data_path() -> Result<PathBuf, ConfigError> {
    Ok(home_dir()?.join(LOTEL_DIR).join("data"))
}

/// Resolve the config file path.
///
/// 1. Check CWD for `lotel-collector.yaml`
/// 2. Fall back to `~/.lotel/collector-config.yaml`
/// 3. Create default config if absent
pub fn resolve_config_path() -> Result<PathBuf, ConfigError> {
    // Check CWD first.
    if let Ok(cwd) = std::env::current_dir() {
        let candidate = cwd.join("lotel-collector.yaml");
        if candidate.exists() {
            return Ok(candidate);
        }
    }

    // Fall back to ~/.lotel/collector-config.yaml.
    let home = home_dir()?;
    let lotel_dir = home.join(LOTEL_DIR);
    fs::create_dir_all(&lotel_dir).map_err(|e| ConfigError::CreateDir {
        path: lotel_dir.clone(),
        source: e,
    })?;

    // Ensure data subdirectories exist.
    let data = lotel_dir.join("data");
    for sub in &["traces", "metrics", "logs"] {
        let p = data.join(sub);
        fs::create_dir_all(&p).map_err(|e| ConfigError::CreateDir { path: p, source: e })?;
    }

    let config_path = lotel_dir.join(DEFAULT_CONFIG_NAME);
    if !config_path.exists() {
        fs::write(&config_path, DEFAULT_CONFIG).map_err(ConfigError::WriteDefault)?;
    }

    Ok(config_path)
}

/// Parse a YAML string into a CollectorConfig.
pub fn parse_config(yaml: &str) -> Result<CollectorConfig, ConfigError> {
    Ok(serde_yaml::from_str(yaml)?)
}

/// Load config from the resolved path.
pub fn load_config() -> Result<CollectorConfig, ConfigError> {
    let path = resolve_config_path()?;
    let content = fs::read_to_string(&path).map_err(|e| ConfigError::ReadFile {
        path: path.clone(),
        source: e,
    })?;
    parse_config(&content)
}

/// Parse a duration string supporting "Nh", "Nm", "Ns", "Nms".
pub fn parse_duration(s: &str) -> std::time::Duration {
    if let Some(ms) = s.strip_suffix("ms")
        && let Ok(n) = ms.parse::<u64>()
    {
        return std::time::Duration::from_millis(n);
    }
    if let Some(hours) = s.strip_suffix('h')
        && let Ok(n) = hours.parse::<f64>()
    {
        return std::time::Duration::from_secs_f64(n * 3600.0);
    }
    if let Some(mins) = s.strip_suffix('m')
        && let Ok(n) = mins.parse::<f64>()
    {
        return std::time::Duration::from_secs_f64(n * 60.0);
    }
    if let Some(secs) = s.strip_suffix('s')
        && let Ok(n) = secs.parse::<f64>()
    {
        return std::time::Duration::from_secs_f64(n);
    }
    std::time::Duration::from_secs(120) // Default 2 minutes.
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_default_config() {
        let config = parse_config(DEFAULT_CONFIG).expect("default config should parse");
        assert_eq!(
            config.receivers.otlp.protocols.grpc.endpoint,
            "0.0.0.0:4317"
        );
        assert_eq!(
            config.receivers.otlp.protocols.http.endpoint,
            "0.0.0.0:4318"
        );
        assert_eq!(config.processors.batch.timeout, "1s");
        assert_eq!(config.processors.batch.send_batch_size, 1024);
        assert_eq!(config.processors.batch.send_batch_max_size, 2048);

        let traces_exporter = config.exporters.get("file/traces").unwrap();
        assert_eq!(traces_exporter.path, "~/.lotel/data/traces/traces.jsonl");
        assert_eq!(traces_exporter.format, "json");

        let metrics_exporter = config.exporters.get("file/metrics").unwrap();
        assert_eq!(metrics_exporter.path, "~/.lotel/data/metrics/metrics.jsonl");

        let logs_exporter = config.exporters.get("file/logs").unwrap();
        assert_eq!(logs_exporter.path, "~/.lotel/data/logs/logs.jsonl");

        assert_eq!(config.extensions.health_check.endpoint, "0.0.0.0:13133");

        assert_eq!(config.service.extensions, vec!["health_check"]);
        assert_eq!(config.service.pipelines.len(), 3);

        let traces_pipeline = config.service.pipelines.get("traces").unwrap();
        assert_eq!(traces_pipeline.receivers, vec!["otlp"]);
        assert_eq!(traces_pipeline.processors, vec!["batch"]);
        assert_eq!(traces_pipeline.exporters, vec!["file/traces"]);

        let ingestion = config.ingestion.as_ref().unwrap();
        assert_eq!(ingestion.interval, "2m");
        assert!(ingestion.enabled);
    }

    #[test]
    fn parse_config_without_ingestion() {
        let yaml = r#"
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
processors:
  batch:
    timeout: 1s
    send_batch_size: 1024
    send_batch_max_size: 2048
exporters:
  file/traces:
    path: /tmp/traces.jsonl
    format: json
extensions:
  health_check:
    endpoint: 0.0.0.0:13133
service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/traces]
"#;
        let config = parse_config(yaml).expect("should parse without ingestion");
        assert!(config.ingestion.is_none());
    }

    #[test]
    fn parse_duration_minutes() {
        assert_eq!(parse_duration("2m"), std::time::Duration::from_secs(120));
        assert_eq!(parse_duration("5m"), std::time::Duration::from_secs(300));
    }

    #[test]
    fn parse_duration_seconds() {
        assert_eq!(parse_duration("30s"), std::time::Duration::from_secs(30));
    }

    #[test]
    fn parse_duration_millis() {
        assert_eq!(
            parse_duration("500ms"),
            std::time::Duration::from_millis(500)
        );
    }

    #[test]
    fn parse_duration_hours() {
        assert_eq!(parse_duration("1h"), std::time::Duration::from_secs(3600));
    }

    #[test]
    fn parse_duration_fallback() {
        assert_eq!(
            parse_duration("invalid"),
            std::time::Duration::from_secs(120)
        );
    }

    #[test]
    fn data_path_is_under_home() {
        let path = data_path().expect("data_path should succeed");
        assert!(path.ends_with(".lotel/data"));
    }
}
