use std::io::{BufRead, BufReader};
use std::path::Path;

use anyhow::{Context, Result};
use duckdb::{Connection, Transaction};
use serde::Deserialize;
use serde_json::Value;

/// Ingest all JSONL files from data_path into the database.
pub fn ingest_all(conn: &Connection, data_path: &Path) -> Result<()> {
    for (signal, ingest_fn) in [
        ("traces", ingest_traces as fn(&Connection, &Path) -> Result<()>),
        ("metrics", ingest_metrics as fn(&Connection, &Path) -> Result<()>),
        ("logs", ingest_logs as fn(&Connection, &Path) -> Result<()>),
    ] {
        let file = data_path.join(signal).join(format!("{signal}.jsonl"));
        if file.exists() {
            ingest_fn(conn, &file).with_context(|| format!("ingesting {signal}"))?;
        }
    }
    Ok(())
}

/// Nanosecond timestamp that handles both string and integer JSON representations.
#[derive(Debug, Clone, Copy, Default, Deserialize)]
#[serde(untagged)]
enum OtlpNano {
    #[default]
    Missing,
    Int(i64),
    Str(#[serde(deserialize_with = "deserialize_nano_str")] i64),
}

fn deserialize_nano_str<'de, D: serde::Deserializer<'de>>(d: D) -> Result<i64, D::Error> {
    let s = String::deserialize(d)?;
    Ok(s.parse().unwrap_or(0))
}

impl OtlpNano {
    fn to_datetime(self) -> Option<chrono::NaiveDateTime> {
        let ns = match self {
            OtlpNano::Int(n) => n,
            OtlpNano::Str(n) => n,
            OtlpNano::Missing => return None,
        };
        if ns == 0 {
            return None;
        }
        let secs = ns / 1_000_000_000;
        let nsecs = (ns % 1_000_000_000) as u32;
        chrono::DateTime::from_timestamp(secs, nsecs).map(|dt| dt.naive_utc())
    }
}

// --- OTLP JSON structures ---

#[derive(Deserialize)]
struct OtlpAttr {
    key: String,
    value: Option<OtlpValue>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct OtlpValue {
    #[serde(alias = "string_value")]
    string_value: Option<String>,
    #[serde(alias = "int_value")]
    int_value: Option<Value>,
    #[serde(alias = "bool_value")]
    bool_value: Option<bool>,
    #[serde(alias = "double_value")]
    double_value: Option<f64>,
}

impl OtlpValue {
    fn as_string(&self) -> String {
        if let Some(s) = &self.string_value {
            return s.clone();
        }
        if let Some(v) = &self.int_value {
            return match v {
                Value::String(s) => s.clone(),
                Value::Number(n) => n.to_string(),
                _ => String::new(),
            };
        }
        if let Some(b) = self.bool_value {
            return b.to_string();
        }
        if let Some(d) = self.double_value {
            return format!("{d}");
        }
        String::new()
    }
}

fn extract_service_name(attrs: &[OtlpAttr]) -> String {
    for attr in attrs {
        if attr.key == "service.name" {
            if let Some(v) = &attr.value {
                return v.as_string();
            }
        }
    }
    "unknown".to_string()
}

fn flatten_attrs(attrs: &[OtlpAttr]) -> serde_json::Value {
    let mut map = serde_json::Map::new();
    for attr in attrs {
        let val = attr
            .value
            .as_ref()
            .map(|v| v.as_string())
            .unwrap_or_default();
        map.insert(attr.key.clone(), Value::String(val));
    }
    Value::Object(map)
}

// --- Traces ingestion ---

// Note: we use rename_all="camelCase" to match standard OTLP JSON format (from Go OTel collector),
// and add alias attributes for snake_case to also accept proto serde output (from Rust exporter).

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct TraceBatch {
    #[serde(alias = "resource_spans")]
    resource_spans: Vec<ResourceSpan>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ResourceSpan {
    resource: Option<Resource>,
    #[serde(alias = "scope_spans")]
    scope_spans: Vec<ScopeSpan>,
}

#[derive(Deserialize)]
struct Resource {
    attributes: Option<Vec<OtlpAttr>>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScopeSpan {
    spans: Vec<SpanJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct SpanJson {
    #[serde(alias = "trace_id")]
    trace_id: Option<String>,
    #[serde(alias = "span_id")]
    span_id: Option<String>,
    #[serde(alias = "parent_span_id")]
    parent_span_id: Option<String>,
    name: Option<String>,
    kind: Option<i32>,
    #[serde(default, alias = "start_time_unix_nano")]
    start_time_unix_nano: OtlpNano,
    #[serde(default, alias = "end_time_unix_nano")]
    end_time_unix_nano: OtlpNano,
    status: Option<SpanStatus>,
    attributes: Option<Vec<OtlpAttr>>,
}

#[derive(Deserialize)]
struct SpanStatus {
    code: Option<i32>,
}

fn ingest_traces(conn: &Connection, file: &Path) -> Result<()> {
    let f = std::fs::File::open(file)?;
    let reader = BufReader::with_capacity(1024 * 1024, f);

    let tx = conn.unchecked_transaction()?;

    for line in reader.lines() {
        let line = line?;
        if line.trim().is_empty() {
            continue;
        }

        let batch: TraceBatch = match serde_json::from_str(&line) {
            Ok(b) => b,
            Err(_) => continue, // Skip malformed lines.
        };

        for rs in &batch.resource_spans {
            let svc_name = rs
                .resource
                .as_ref()
                .and_then(|r| r.attributes.as_ref())
                .map(|a| extract_service_name(a))
                .unwrap_or_else(|| "unknown".to_string());

            for ss in &rs.scope_spans {
                for span in &ss.spans {
                    insert_span(&tx, span, &svc_name)?;
                }
            }
        }
    }

    tx.commit()?;
    Ok(())
}

fn insert_span(tx: &Transaction, span: &SpanJson, svc_name: &str) -> Result<()> {
    let start_time = span.start_time_unix_nano.to_datetime();
    let end_time = span.end_time_unix_nano.to_datetime();
    let duration_ns = match (start_time, end_time) {
        (Some(s), Some(e)) => (e - s).num_nanoseconds().unwrap_or(0),
        _ => 0,
    };
    let attrs = span
        .attributes
        .as_ref()
        .map(|a| flatten_attrs(a))
        .unwrap_or(Value::Object(serde_json::Map::new()));
    let attrs_str = serde_json::to_string(&attrs)?;
    let date_str = start_time.map(|t| t.format("%Y-%m-%d").to_string());

    tx.execute(
        "INSERT INTO traces (trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
        duckdb::params![
            span.trace_id.as_deref().unwrap_or(""),
            span.span_id.as_deref().unwrap_or(""),
            span.parent_span_id.as_deref(),
            span.name.as_deref().unwrap_or(""),
            span.kind.unwrap_or(0),
            start_time,
            end_time,
            duration_ns,
            span.status.as_ref().and_then(|s| s.code).unwrap_or(0),
            svc_name,
            attrs_str,
            date_str.as_deref(),
        ],
    )?;
    Ok(())
}

// --- Metrics ingestion ---

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MetricBatch {
    #[serde(alias = "resource_metrics")]
    resource_metrics: Vec<ResourceMetric>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ResourceMetric {
    resource: Option<Resource>,
    #[serde(alias = "scope_metrics")]
    scope_metrics: Vec<ScopeMetric>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScopeMetric {
    metrics: Vec<MetricJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MetricJson {
    name: String,
    unit: Option<String>,
    sum: Option<SumJson>,
    gauge: Option<GaugeJson>,
    histogram: Option<HistogramJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct SumJson {
    #[serde(alias = "data_points")]
    data_points: Vec<DataPointJson>,
    #[serde(alias = "aggregation_temporality")]
    aggregation_temporality: Option<i32>,
    #[serde(alias = "is_monotonic")]
    is_monotonic: Option<bool>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct GaugeJson {
    #[serde(alias = "data_points")]
    data_points: Vec<DataPointJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct HistogramJson {
    #[serde(alias = "data_points")]
    data_points: Vec<HistogramDPJson>,
    #[serde(alias = "aggregation_temporality")]
    aggregation_temporality: Option<i32>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct DataPointJson {
    attributes: Option<Vec<OtlpAttr>>,
    #[serde(default, alias = "time_unix_nano")]
    time_unix_nano: OtlpNano,
    #[serde(alias = "as_int")]
    as_int: Option<Value>,
    #[serde(alias = "as_double")]
    as_double: Option<f64>,
}

impl DataPointJson {
    fn value(&self) -> f64 {
        if let Some(d) = self.as_double {
            return d;
        }
        if let Some(v) = &self.as_int {
            return match v {
                Value::String(s) => s.parse().unwrap_or(0.0),
                Value::Number(n) => n.as_f64().unwrap_or(0.0),
                _ => 0.0,
            };
        }
        0.0
    }
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct HistogramDPJson {
    attributes: Option<Vec<OtlpAttr>>,
    #[serde(default, alias = "time_unix_nano")]
    time_unix_nano: OtlpNano,
    sum: Option<f64>,
}

struct MetricPoint {
    metric_type: &'static str,
    value: f64,
    timestamp: Option<chrono::NaiveDateTime>,
    temporality: Option<i32>,
    monotonic: Option<bool>,
    attributes: serde_json::Value,
}

fn extract_data_points(m: &MetricJson) -> Vec<MetricPoint> {
    let mut points = Vec::new();

    if let Some(sum) = &m.sum {
        for dp in &sum.data_points {
            points.push(MetricPoint {
                metric_type: "sum",
                value: dp.value(),
                timestamp: dp.time_unix_nano.to_datetime(),
                temporality: sum.aggregation_temporality,
                monotonic: sum.is_monotonic,
                attributes: dp
                    .attributes
                    .as_ref()
                    .map(|a| flatten_attrs(a))
                    .unwrap_or(Value::Object(serde_json::Map::new())),
            });
        }
    }

    if let Some(gauge) = &m.gauge {
        for dp in &gauge.data_points {
            points.push(MetricPoint {
                metric_type: "gauge",
                value: dp.value(),
                timestamp: dp.time_unix_nano.to_datetime(),
                temporality: None,
                monotonic: None,
                attributes: dp
                    .attributes
                    .as_ref()
                    .map(|a| flatten_attrs(a))
                    .unwrap_or(Value::Object(serde_json::Map::new())),
            });
        }
    }

    if let Some(hist) = &m.histogram {
        for dp in &hist.data_points {
            points.push(MetricPoint {
                metric_type: "histogram",
                value: dp.sum.unwrap_or(0.0),
                timestamp: dp.time_unix_nano.to_datetime(),
                temporality: hist.aggregation_temporality,
                monotonic: None,
                attributes: dp
                    .attributes
                    .as_ref()
                    .map(|a| flatten_attrs(a))
                    .unwrap_or(Value::Object(serde_json::Map::new())),
            });
        }
    }

    points
}

fn ingest_metrics(conn: &Connection, file: &Path) -> Result<()> {
    let f = std::fs::File::open(file)?;
    let reader = BufReader::with_capacity(1024 * 1024, f);

    let tx = conn.unchecked_transaction()?;

    for line in reader.lines() {
        let line = line?;
        if line.trim().is_empty() {
            continue;
        }

        let batch: MetricBatch = match serde_json::from_str(&line) {
            Ok(b) => b,
            Err(_) => continue,
        };

        for rm in &batch.resource_metrics {
            let svc_name = rm
                .resource
                .as_ref()
                .and_then(|r| r.attributes.as_ref())
                .map(|a| extract_service_name(a))
                .unwrap_or_else(|| "unknown".to_string());

            for sm in &rm.scope_metrics {
                for m in &sm.metrics {
                    for dp in extract_data_points(m) {
                        let attrs_str = serde_json::to_string(&dp.attributes)?;
                        let date_str = dp.timestamp.map(|t| t.format("%Y-%m-%d").to_string());

                        tx.execute(
                            "INSERT INTO metrics (metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                            duckdb::params![
                                m.name,
                                dp.metric_type,
                                dp.value,
                                dp.timestamp,
                                svc_name,
                                dp.temporality,
                                dp.monotonic,
                                m.unit.as_deref(),
                                attrs_str,
                                date_str.as_deref(),
                            ],
                        )?;
                    }
                }
            }
        }
    }

    tx.commit()?;
    Ok(())
}

// --- Logs ingestion ---

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct LogBatch {
    #[serde(alias = "resource_logs")]
    resource_logs: Vec<ResourceLog>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ResourceLog {
    resource: Option<Resource>,
    #[serde(alias = "scope_logs")]
    scope_logs: Vec<ScopeLog>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScopeLog {
    #[serde(alias = "log_records")]
    log_records: Vec<LogRecordJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct LogRecordJson {
    #[serde(default, alias = "time_unix_nano")]
    time_unix_nano: OtlpNano,
    #[serde(alias = "severity_text")]
    severity_text: Option<String>,
    #[serde(alias = "severity_number")]
    severity_number: Option<i32>,
    body: Option<OtlpValue>,
    #[serde(alias = "trace_id")]
    trace_id: Option<String>,
    #[serde(alias = "span_id")]
    span_id: Option<String>,
    attributes: Option<Vec<OtlpAttr>>,
}

fn ingest_logs(conn: &Connection, file: &Path) -> Result<()> {
    let f = std::fs::File::open(file)?;
    let reader = BufReader::with_capacity(1024 * 1024, f);

    let tx = conn.unchecked_transaction()?;

    for line in reader.lines() {
        let line = line?;
        if line.trim().is_empty() {
            continue;
        }

        let batch: LogBatch = match serde_json::from_str(&line) {
            Ok(b) => b,
            Err(_) => continue,
        };

        for rl in &batch.resource_logs {
            let svc_name = rl
                .resource
                .as_ref()
                .and_then(|r| r.attributes.as_ref())
                .map(|a| extract_service_name(a))
                .unwrap_or_else(|| "unknown".to_string());

            for sl in &rl.scope_logs {
                for lr in &sl.log_records {
                    let ts = lr.time_unix_nano.to_datetime();
                    let attrs = lr
                        .attributes
                        .as_ref()
                        .map(|a| flatten_attrs(a))
                        .unwrap_or(Value::Object(serde_json::Map::new()));
                    let attrs_str = serde_json::to_string(&attrs)?;
                    let body_str = lr.body.as_ref().map(|b| b.as_string());
                    let date_str = ts.map(|t| t.format("%Y-%m-%d").to_string());
                    let trace_id = lr.trace_id.as_deref().filter(|s| !s.is_empty());
                    let span_id = lr.span_id.as_deref().filter(|s| !s.is_empty());

                    tx.execute(
                        "INSERT INTO logs (timestamp, severity, severity_number, body, service_name, trace_id, span_id, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                        duckdb::params![
                            ts,
                            lr.severity_text.as_deref(),
                            lr.severity_number,
                            body_str.as_deref(),
                            svc_name,
                            trace_id,
                            span_id,
                            attrs_str,
                            date_str.as_deref(),
                        ],
                    )?;
                }
            }
        }
    }

    tx.commit()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db;
    use std::io::Write;

    fn setup_db() -> Connection {
        db::open_in_memory().unwrap()
    }

    #[test]
    fn ingest_traces_jsonl() {
        let conn = setup_db();
        let tmp = tempfile::TempDir::new().unwrap();
        let traces_dir = tmp.path().join("traces");
        std::fs::create_dir_all(&traces_dir).unwrap();
        let file = traces_dir.join("traces.jsonl");

        let data = r#"{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-svc"}}]},"scopeSpans":[{"spans":[{"traceId":"abc123","spanId":"def456","name":"test-span","kind":1,"startTimeUnixNano":"1710000000000000000","endTimeUnixNano":"1710000001000000000","status":{"code":0},"attributes":[{"key":"http.method","value":{"stringValue":"GET"}}]}]}]}]}"#;
        std::fs::write(&file, format!("{data}\n")).unwrap();

        ingest_traces(&conn, &file).unwrap();

        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM traces", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 1);

        let name: String = conn
            .query_row("SELECT name FROM traces LIMIT 1", [], |row| row.get(0))
            .unwrap();
        assert_eq!(name, "test-span");

        let svc: String = conn
            .query_row("SELECT service_name FROM traces LIMIT 1", [], |row| {
                row.get(0)
            })
            .unwrap();
        assert_eq!(svc, "test-svc");
    }

    #[test]
    fn ingest_metrics_jsonl() {
        let conn = setup_db();
        let tmp = tempfile::TempDir::new().unwrap();
        let metrics_dir = tmp.path().join("metrics");
        std::fs::create_dir_all(&metrics_dir).unwrap();
        let file = metrics_dir.join("metrics.jsonl");

        let data = r#"{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-svc"}}]},"scopeMetrics":[{"metrics":[{"name":"http.requests","unit":"1","sum":{"dataPoints":[{"timeUnixNano":"1710000000000000000","asDouble":42.0,"attributes":[]}],"aggregationTemporality":2,"isMonotonic":true}}]}]}]}"#;
        std::fs::write(&file, format!("{data}\n")).unwrap();

        ingest_metrics(&conn, &file).unwrap();

        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM metrics", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 1);

        let value: f64 = conn
            .query_row("SELECT value FROM metrics LIMIT 1", [], |row| row.get(0))
            .unwrap();
        assert!((value - 42.0).abs() < f64::EPSILON);
    }

    #[test]
    fn ingest_logs_jsonl() {
        let conn = setup_db();
        let tmp = tempfile::TempDir::new().unwrap();
        let logs_dir = tmp.path().join("logs");
        std::fs::create_dir_all(&logs_dir).unwrap();
        let file = logs_dir.join("logs.jsonl");

        let data = r#"{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-svc"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1710000000000000000","severityText":"INFO","severityNumber":9,"body":{"stringValue":"hello world"},"attributes":[]}]}]}]}"#;
        std::fs::write(&file, format!("{data}\n")).unwrap();

        ingest_logs(&conn, &file).unwrap();

        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM logs", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 1);

        let body: String = conn
            .query_row("SELECT body FROM logs LIMIT 1", [], |row| row.get(0))
            .unwrap();
        assert_eq!(body, "hello world");
    }

    #[test]
    fn ingest_all_skips_missing() {
        let conn = setup_db();
        let tmp = tempfile::TempDir::new().unwrap();
        // No files exist — should not error.
        ingest_all(&conn, tmp.path()).unwrap();
    }
}
