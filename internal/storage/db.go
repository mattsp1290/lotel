package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/marcboeker/go-duckdb"
)

var (
	dbOnce sync.Once
	dbInst *sql.DB
	dbErr  error
)

// DB returns the singleton DuckDB connection.
func DB() (*sql.DB, error) {
	dbOnce.Do(func() {
		dbPath, err := dbPath()
		if err != nil {
			dbErr = err
			return
		}
		dbInst, dbErr = sql.Open("duckdb", dbPath)
		if dbErr != nil {
			return
		}
		dbErr = migrate(dbInst)
	})
	return dbInst, dbErr
}

// OpenDB opens a DuckDB connection to the given path (for testing).
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func dbPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	dir := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating data directory: %w", err)
	}
	return filepath.Join(dir, "lotel.db"), nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS traces (
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
		)`,
		`CREATE TABLE IF NOT EXISTS metrics (
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
		)`,
		`CREATE TABLE IF NOT EXISTS logs (
			timestamp       TIMESTAMP NOT NULL,
			severity        VARCHAR,
			severity_number INTEGER,
			body            VARCHAR,
			service_name    VARCHAR NOT NULL,
			trace_id        VARCHAR,
			span_id         VARCHAR,
			attributes      JSON,
			date            DATE NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return nil
}
