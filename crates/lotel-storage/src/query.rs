use anyhow::{Context, Result};
use chrono::NaiveDateTime;
use duckdb::Connection;
use serde::{Deserialize, Serialize};

/// Common query parameters.
#[derive(Debug, Default)]
pub struct QueryOptions {
    pub service: Option<String>,
    pub since: Option<NaiveDateTime>,
    pub until: Option<NaiveDateTime>,
    pub limit: Option<usize>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct TraceResult {
    pub trace_id: String,
    pub span_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_span_id: Option<String>,
    pub name: String,
    pub kind: i32,
    pub start_time: NaiveDateTime,
    pub end_time: Option<NaiveDateTime>,
    pub duration_ns: i64,
    pub status_code: i32,
    pub service_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub attributes: Option<serde_json::Value>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct MetricResult {
    pub metric_name: String,
    pub metric_type: String,
    pub value: f64,
    pub timestamp: NaiveDateTime,
    pub service_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub aggregation_temporality: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub is_monotonic: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub unit: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub attributes: Option<serde_json::Value>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct LogResult {
    pub timestamp: NaiveDateTime,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub severity: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub severity_number: Option<i32>,
    pub body: Option<String>,
    pub service_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub trace_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub span_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub attributes: Option<serde_json::Value>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct MetricAggregation {
    pub metric_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub service_name: Option<String>,
    pub count: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub avg: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub min: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max: Option<f64>,
}

pub fn query_traces(conn: &Connection, opts: &QueryOptions) -> Result<Vec<TraceResult>> {
    let mut query = String::from(
        "SELECT trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, CAST(attributes AS VARCHAR) FROM traces WHERE 1=1",
    );
    let mut params: Vec<Box<dyn duckdb::types::ToSql>> = Vec::new();

    append_where(&mut query, &mut params, opts, "start_time");

    query.push_str(" ORDER BY start_time ASC");
    if let Some(limit) = opts.limit
        && limit > 0
    {
        query.push_str(&format!(" LIMIT {limit}"));
    }

    let mut stmt = conn.prepare(&query)?;
    let param_refs: Vec<&dyn duckdb::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
    let rows = stmt
        .query_map(param_refs.as_slice(), |row| {
            Ok(TraceResult {
                trace_id: row.get(0)?,
                span_id: row.get(1)?,
                parent_span_id: row.get(2)?,
                name: row.get(3)?,
                kind: row.get(4)?,
                start_time: row.get(5)?,
                end_time: row.get(6)?,
                duration_ns: row.get(7)?,
                status_code: row.get(8)?,
                service_name: row.get(9)?,
                attributes: row
                    .get::<_, Option<String>>(10)?
                    .and_then(|s| serde_json::from_str(&s).ok()),
            })
        })
        .context("querying traces")?;

    rows.map(|r| r.map_err(Into::into)).collect()
}

pub fn query_metrics(conn: &Connection, opts: &QueryOptions) -> Result<Vec<MetricResult>> {
    let mut query = String::from(
        "SELECT metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, CAST(attributes AS VARCHAR) FROM metrics WHERE 1=1",
    );
    let mut params: Vec<Box<dyn duckdb::types::ToSql>> = Vec::new();

    append_where(&mut query, &mut params, opts, "timestamp");

    query.push_str(" ORDER BY timestamp ASC");
    if let Some(limit) = opts.limit
        && limit > 0
    {
        query.push_str(&format!(" LIMIT {limit}"));
    }

    let mut stmt = conn.prepare(&query)?;
    let param_refs: Vec<&dyn duckdb::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
    let rows = stmt
        .query_map(param_refs.as_slice(), |row| {
            Ok(MetricResult {
                metric_name: row.get(0)?,
                metric_type: row.get(1)?,
                value: row.get(2)?,
                timestamp: row.get(3)?,
                service_name: row.get(4)?,
                aggregation_temporality: row.get(5)?,
                is_monotonic: row.get(6)?,
                unit: row.get(7)?,
                attributes: row
                    .get::<_, Option<String>>(8)?
                    .and_then(|s| serde_json::from_str(&s).ok()),
            })
        })
        .context("querying metrics")?;

    rows.map(|r| r.map_err(Into::into)).collect()
}

pub fn query_logs(conn: &Connection, opts: &QueryOptions) -> Result<Vec<LogResult>> {
    let mut query = String::from(
        "SELECT timestamp, severity, severity_number, body, service_name, trace_id, span_id, CAST(attributes AS VARCHAR) FROM logs WHERE 1=1",
    );
    let mut params: Vec<Box<dyn duckdb::types::ToSql>> = Vec::new();

    append_where(&mut query, &mut params, opts, "timestamp");

    query.push_str(" ORDER BY timestamp ASC");
    if let Some(limit) = opts.limit
        && limit > 0
    {
        query.push_str(&format!(" LIMIT {limit}"));
    }

    let mut stmt = conn.prepare(&query)?;
    let param_refs: Vec<&dyn duckdb::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
    let rows = stmt
        .query_map(param_refs.as_slice(), |row| {
            Ok(LogResult {
                timestamp: row.get(0)?,
                severity: row.get(1)?,
                severity_number: row.get(2)?,
                body: row.get(3)?,
                service_name: row.get(4)?,
                trace_id: row.get(5)?,
                span_id: row.get(6)?,
                attributes: row
                    .get::<_, Option<String>>(7)?
                    .and_then(|s| serde_json::from_str(&s).ok()),
            })
        })
        .context("querying logs")?;

    rows.map(|r| r.map_err(Into::into)).collect()
}

pub fn aggregate_metrics(
    conn: &Connection,
    opts: &QueryOptions,
    metric_name: &str,
) -> Result<MetricAggregation> {
    let mut query = String::from(
        "SELECT COUNT(*), AVG(value), MIN(value), MAX(value) FROM metrics WHERE metric_name = ?",
    );
    let mut params: Vec<Box<dyn duckdb::types::ToSql>> = Vec::new();
    params.push(Box::new(metric_name.to_string()));

    if let Some(ref svc) = opts.service {
        query.push_str(" AND service_name = ?");
        params.push(Box::new(svc.clone()));
    }
    if let Some(since) = opts.since {
        query.push_str(" AND timestamp >= ?");
        params.push(Box::new(since));
    }
    if let Some(until) = opts.until {
        query.push_str(" AND timestamp <= ?");
        params.push(Box::new(until));
    }

    let param_refs: Vec<&dyn duckdb::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
    conn.query_row(&query, param_refs.as_slice(), |row| {
        Ok(MetricAggregation {
            metric_name: metric_name.to_string(),
            service_name: opts.service.clone(),
            count: row.get(0)?,
            avg: row.get(1)?,
            min: row.get(2)?,
            max: row.get(3)?,
        })
    })
    .context("aggregating metrics")
}

fn append_where(
    query: &mut String,
    params: &mut Vec<Box<dyn duckdb::types::ToSql>>,
    opts: &QueryOptions,
    time_col: &str,
) {
    if let Some(ref svc) = opts.service {
        query.push_str(" AND service_name = ?");
        params.push(Box::new(svc.clone()));
    }
    if let Some(since) = opts.since {
        query.push_str(&format!(" AND {time_col} >= ?"));
        params.push(Box::new(since));
    }
    if let Some(until) = opts.until {
        query.push_str(&format!(" AND {time_col} <= ?"));
        params.push(Box::new(until));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db;

    fn setup_with_data() -> Connection {
        let conn = db::open_in_memory().unwrap();
        conn.execute(
            "INSERT INTO traces VALUES ('t1', 's1', NULL, 'span-1', 1, '2024-03-09 16:00:00', '2024-03-09 16:00:01', 1000000000, 0, 'svc-a', '{\"k\":\"v\"}', '2024-03-09')",
            [],
        ).unwrap();
        conn.execute(
            "INSERT INTO traces VALUES ('t2', 's2', 's1', 'span-2', 2, '2024-03-09 17:00:00', '2024-03-09 17:00:02', 2000000000, 0, 'svc-b', '{}', '2024-03-09')",
            [],
        ).unwrap();
        conn.execute(
            "INSERT INTO metrics VALUES ('http.requests', 'sum', 42.0, '2024-03-09 16:00:00', 'svc-a', 2, true, '1', '{}', '2024-03-09')",
            [],
        ).unwrap();
        conn.execute(
            "INSERT INTO logs VALUES ('2024-03-09 16:00:00', 'INFO', 9, 'hello', 'svc-a', 't1', 's1', '{}', '2024-03-09')",
            [],
        ).unwrap();
        conn
    }

    #[test]
    fn query_traces_all() {
        let conn = setup_with_data();
        let results = query_traces(&conn, &QueryOptions::default()).unwrap();
        assert_eq!(results.len(), 2);
        assert_eq!(results[0].name, "span-1");
        assert_eq!(results[1].name, "span-2");
    }

    #[test]
    fn query_traces_with_service_filter() {
        let conn = setup_with_data();
        let opts = QueryOptions {
            service: Some("svc-a".to_string()),
            ..Default::default()
        };
        let results = query_traces(&conn, &opts).unwrap();
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].service_name, "svc-a");
    }

    #[test]
    fn query_traces_with_limit() {
        let conn = setup_with_data();
        let opts = QueryOptions {
            limit: Some(1),
            ..Default::default()
        };
        let results = query_traces(&conn, &opts).unwrap();
        assert_eq!(results.len(), 1);
    }

    #[test]
    fn query_metrics_all() {
        let conn = setup_with_data();
        let results = query_metrics(&conn, &QueryOptions::default()).unwrap();
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].metric_name, "http.requests");
        assert!((results[0].value - 42.0).abs() < f64::EPSILON);
    }

    #[test]
    fn query_logs_all() {
        let conn = setup_with_data();
        let results = query_logs(&conn, &QueryOptions::default()).unwrap();
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].body.as_deref(), Some("hello"));
    }

    #[test]
    fn aggregate_metrics_basic() {
        let conn = setup_with_data();
        let agg = aggregate_metrics(&conn, &QueryOptions::default(), "http.requests").unwrap();
        assert_eq!(agg.count, 1);
        assert!((agg.avg.unwrap() - 42.0).abs() < f64::EPSILON);
    }
}
