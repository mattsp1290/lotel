package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

const schemaVersion = 3

var SchemaRebuilt bool

func migrate(db *sql.DB) error {
	SchemaRebuilt = false
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (key VARCHAR PRIMARY KEY, value VARCHAR)`); err != nil {
		return err
	}
	var versionText string
	err = tx.QueryRow(`SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&versionText)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	version := 0
	if err == nil {
		version, err = strconv.Atoi(versionText)
		if err != nil {
			return fmt.Errorf("invalid database schema version: %w", err)
		}
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema v%d is newer than this lotel (v3); upgrade lotel or delete the database", version)
	}
	rebuilt := false
	if version < 2 {
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='traces')`).Scan(&rebuilt); err != nil {
			return err
		}
		for _, table := range []string{"traces", "metrics", "logs", "ingest_state"} {
			if _, err := tx.Exec("DROP TABLE IF EXISTS " + table); err != nil {
				return err
			}
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS traces (
			trace_id       VARCHAR NOT NULL,
			span_id        VARCHAR NOT NULL,
			parent_span_id VARCHAR,
   trace_state VARCHAR,
   status_message VARCHAR,
   resource_attributes JSON,
   scope_name VARCHAR,
   scope_version VARCHAR,
   schema_url VARCHAR,
   events JSON,
   links JSON,
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
   description VARCHAR,
   start_timestamp TIMESTAMP,
   "count" BIGINT,
   "sum" DOUBLE,
   "min" DOUBLE,
   "max" DOUBLE,
   explicit_bounds JSON,
   bucket_counts JSON,
   exp_scale INTEGER,
   exp_zero_count BIGINT,
   exp_positive JSON,
   exp_negative JSON,
   quantiles JSON,
   resource_attributes JSON,
   scope_name VARCHAR,
   scope_version VARCHAR,
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
			observed_timestamp TIMESTAMP,
   event_name VARCHAR,
   body_json JSON,
   flags BIGINT,
   resource_attributes JSON,
   scope_name VARCHAR,
   scope_version VARCHAR,
   severity        VARCHAR,
			severity_number INTEGER,
			body            VARCHAR,
			service_name    VARCHAR NOT NULL,
			trace_id        VARCHAR,
			span_id         VARCHAR,
			attributes      JSON,
			date            DATE NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ingest_state (
   path VARCHAR PRIMARY KEY,
   byte_offset BIGINT,
   fingerprint VARCHAR,
   fingerprint_len INTEGER,
   updated_at TIMESTAMP,
   raw_byte_offset BIGINT DEFAULT 0
  )`,
		`CREATE TABLE IF NOT EXISTS otlp_envelopes (
            signal VARCHAR NOT NULL,
            service_name VARCHAR NOT NULL,
            first_nano UBIGINT,
            last_nano UBIGINT,
            payload JSON NOT NULL
        )`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	if version == 2 {
		if _, err := tx.Exec(`ALTER TABLE ingest_state ADD COLUMN raw_byte_offset BIGINT DEFAULT 0`); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE ingest_state SET raw_byte_offset=byte_offset`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE logs ALTER COLUMN flags TYPE BIGINT`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_meta VALUES ('schema_version','3') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	SchemaRebuilt = rebuilt
	return nil
}
