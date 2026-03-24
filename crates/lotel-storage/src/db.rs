use std::fs;
use std::path::Path;

use duckdb::Connection;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum StorageError {
    #[error("getting home directory")]
    NoHome,
    #[error("creating directory {path}: {source}")]
    CreateDir {
        path: String,
        source: std::io::Error,
    },
    #[error("duckdb error: {0}")]
    DuckDb(#[from] duckdb::Error),
}

/// Open a DuckDB connection at the given path, creating parent directories
/// and running migrations.
pub fn open_db(path: &Path) -> Result<Connection, StorageError> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|e| StorageError::CreateDir {
            path: parent.display().to_string(),
            source: e,
        })?;
    }
    let conn = Connection::open(path)?;
    migrate(&conn)?;
    Ok(conn)
}

/// Open an in-memory DuckDB with migrations applied (for testing).
pub fn open_in_memory() -> Result<Connection, StorageError> {
    let conn = Connection::open_in_memory()?;
    migrate(&conn)?;
    Ok(conn)
}

/// Open the default DuckDB at ~/.lotel/data/lotel.db.
pub fn default_db() -> Result<Connection, StorageError> {
    let home = dirs::home_dir().ok_or(StorageError::NoHome)?;
    let db_path = home.join(".lotel").join("data").join("lotel.db");
    open_db(&db_path)
}

/// Run schema migrations, creating tables if they don't exist.
fn migrate(conn: &Connection) -> Result<(), StorageError> {
    let stmts = [
        "CREATE TABLE IF NOT EXISTS traces (
            trace_id       VARCHAR NOT NULL,
            span_id        VARCHAR NOT NULL,
            parent_span_id VARCHAR,
            name           VARCHAR NOT NULL,
            kind           INTEGER,
            start_time     TIMESTAMP NOT NULL,
            end_time       TIMESTAMP,
            duration_ns    BIGINT,
            status_code    INTEGER,
            service_name   VARCHAR NOT NULL,
            attributes     JSON,
            date           DATE NOT NULL
        )",
        "CREATE TABLE IF NOT EXISTS metrics (
            metric_name              VARCHAR NOT NULL,
            metric_type              VARCHAR NOT NULL,
            value                    DOUBLE,
            timestamp                TIMESTAMP NOT NULL,
            service_name             VARCHAR NOT NULL,
            aggregation_temporality  INTEGER,
            is_monotonic             BOOLEAN,
            unit                     VARCHAR,
            attributes               JSON,
            date                     DATE NOT NULL
        )",
        "CREATE TABLE IF NOT EXISTS logs (
            timestamp       TIMESTAMP NOT NULL,
            severity        VARCHAR,
            severity_number INTEGER,
            body            VARCHAR,
            service_name    VARCHAR NOT NULL,
            trace_id        VARCHAR,
            span_id         VARCHAR,
            attributes      JSON,
            date            DATE NOT NULL
        )",
        // Cursors survive prune operations intentionally — they track JSONL file
        // byte offsets to prevent re-ingestion. Only `lotel ingest --full` resets them.
        "CREATE TABLE IF NOT EXISTS ingest_cursors (
            file_path    VARCHAR NOT NULL PRIMARY KEY,
            byte_offset  UBIGINT NOT NULL
        )",
    ];
    for stmt in &stmts {
        conn.execute(stmt, [])?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn in_memory_db() -> Connection {
        let conn = Connection::open_in_memory().expect("open in-memory db");
        migrate(&conn).expect("migration should succeed");
        conn
    }

    #[test]
    fn migration_creates_tables() {
        let conn = in_memory_db();
        let tables: Vec<String> = conn
            .prepare("SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name")
            .unwrap()
            .query_map([], |row| row.get(0))
            .unwrap()
            .map(|r| r.unwrap())
            .collect();
        assert_eq!(tables, vec!["ingest_cursors", "logs", "metrics", "traces"]);
    }

    #[test]
    fn migration_is_idempotent() {
        let conn = Connection::open_in_memory().expect("open in-memory db");
        migrate(&conn).expect("first migration");
        migrate(&conn).expect("second migration should also succeed");
    }

    #[test]
    fn traces_columns() {
        let conn = in_memory_db();
        let cols: Vec<String> = conn
            .prepare("SELECT column_name FROM information_schema.columns WHERE table_name = 'traces' ORDER BY ordinal_position")
            .unwrap()
            .query_map([], |row| row.get(0))
            .unwrap()
            .map(|r| r.unwrap())
            .collect();
        assert_eq!(
            cols,
            vec![
                "trace_id",
                "span_id",
                "parent_span_id",
                "name",
                "kind",
                "start_time",
                "end_time",
                "duration_ns",
                "status_code",
                "service_name",
                "attributes",
                "date"
            ]
        );
    }
}
