use anyhow::{Context, Result};
use chrono::NaiveDateTime;
use duckdb::Connection;
use serde::Serialize;

#[derive(Debug, Serialize)]
pub struct PruneReport {
    pub signal: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub service_name: Option<String>,
    pub deleted: i64,
    pub cutoff: String,
}

/// Prune telemetry data older than `cutoff`.
/// If `dry_run`, returns what would be deleted without deleting.
pub fn prune(
    conn: &Connection,
    cutoff: NaiveDateTime,
    service: Option<&str>,
    dry_run: bool,
) -> Result<Vec<PruneReport>> {
    let signals = [
        ("traces", "start_time"),
        ("metrics", "timestamp"),
        ("logs", "timestamp"),
    ];

    let cutoff_str = cutoff.format("%Y-%m-%dT%H:%M:%S").to_string();
    let mut reports = Vec::new();

    for (signal, time_col) in &signals {
        let mut count_query = format!("SELECT COUNT(*) FROM {signal} WHERE {time_col} < ?");
        let mut params: Vec<Box<dyn duckdb::types::ToSql>> = Vec::new();
        params.push(Box::new(cutoff));

        if let Some(svc) = service {
            count_query.push_str(" AND service_name = ?");
            params.push(Box::new(svc.to_string()));
        }

        let param_refs: Vec<&dyn duckdb::types::ToSql> =
            params.iter().map(|p| p.as_ref()).collect();
        let count: i64 = conn
            .query_row(&count_query, param_refs.as_slice(), |row| row.get(0))
            .with_context(|| format!("counting {signal} for prune"))?;

        if !dry_run && count > 0 {
            let mut delete_query = format!("DELETE FROM {signal} WHERE {time_col} < ?");
            let mut del_params: Vec<Box<dyn duckdb::types::ToSql>> = Vec::new();
            del_params.push(Box::new(cutoff));

            if let Some(svc) = service {
                delete_query.push_str(" AND service_name = ?");
                del_params.push(Box::new(svc.to_string()));
            }

            let del_refs: Vec<&dyn duckdb::types::ToSql> =
                del_params.iter().map(|p| p.as_ref()).collect();
            conn.execute(&delete_query, del_refs.as_slice())
                .with_context(|| format!("pruning {signal}"))?;
        }

        reports.push(PruneReport {
            signal: signal.to_string(),
            service_name: service.map(String::from),
            deleted: count,
            cutoff: cutoff_str.clone(),
        });
    }

    Ok(reports)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db;

    fn setup_with_data() -> Connection {
        let conn = db::open_in_memory().unwrap();
        conn.execute(
            "INSERT INTO traces VALUES ('t1', 's1', NULL, 'old', 1, '2024-01-01 00:00:00', '2024-01-01 00:00:01', 1000000000, 0, 'svc-a', '{}', '2024-01-01')",
            [],
        ).unwrap();
        conn.execute(
            "INSERT INTO traces VALUES ('t2', 's2', NULL, 'new', 1, '2024-12-01 00:00:00', '2024-12-01 00:00:01', 1000000000, 0, 'svc-a', '{}', '2024-12-01')",
            [],
        ).unwrap();
        conn.execute(
            "INSERT INTO metrics VALUES ('m1', 'sum', 1.0, '2024-01-01 00:00:00', 'svc-a', NULL, NULL, NULL, '{}', '2024-01-01')",
            [],
        ).unwrap();
        conn.execute(
            "INSERT INTO logs VALUES ('2024-01-01 00:00:00', 'INFO', 9, 'old log', 'svc-a', NULL, NULL, '{}', '2024-01-01')",
            [],
        ).unwrap();
        conn
    }

    #[test]
    fn dry_run_does_not_delete() {
        let conn = setup_with_data();
        let cutoff =
            NaiveDateTime::parse_from_str("2024-06-01 00:00:00", "%Y-%m-%d %H:%M:%S").unwrap();
        let reports = prune(&conn, cutoff, None, true).unwrap();

        assert_eq!(reports.len(), 3);
        assert_eq!(reports[0].signal, "traces");
        assert_eq!(reports[0].deleted, 1); // The old trace.

        // Verify nothing was actually deleted.
        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM traces", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 2);
    }

    #[test]
    fn prune_deletes_old_data() {
        let conn = setup_with_data();
        let cutoff =
            NaiveDateTime::parse_from_str("2024-06-01 00:00:00", "%Y-%m-%d %H:%M:%S").unwrap();
        let reports = prune(&conn, cutoff, None, false).unwrap();

        assert_eq!(reports[0].deleted, 1); // Old trace deleted.
        assert_eq!(reports[1].deleted, 1); // Old metric deleted.
        assert_eq!(reports[2].deleted, 1); // Old log deleted.

        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM traces", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 1); // Only the new trace remains.
    }

    #[test]
    fn prune_with_service_filter() {
        let conn = setup_with_data();
        // Add data for a different service.
        conn.execute(
            "INSERT INTO traces VALUES ('t3', 's3', NULL, 'other', 1, '2024-01-01 00:00:00', '2024-01-01 00:00:01', 1000000000, 0, 'svc-b', '{}', '2024-01-01')",
            [],
        ).unwrap();

        let cutoff =
            NaiveDateTime::parse_from_str("2024-06-01 00:00:00", "%Y-%m-%d %H:%M:%S").unwrap();
        let reports = prune(&conn, cutoff, Some("svc-a"), false).unwrap();

        assert_eq!(reports[0].deleted, 1); // Only svc-a trace deleted.

        // svc-b trace should still exist.
        let count: i64 = conn
            .query_row(
                "SELECT COUNT(*) FROM traces WHERE service_name = 'svc-b'",
                [],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(count, 1);
    }
}
