package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// ReingestAll atomically rebuilds indexed telemetry from retained JSONL files.
// Rows removed by pruning return when their original JSONL remains available.
func ReingestAll(db *sql.DB, dataPath string) (*IngestReport, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Reusing cursor rows avoids DuckDB dropping deleted keys during a same-transaction upsert.
	if _, err := tx.Exec("UPDATE ingest_state SET byte_offset=0"); err != nil {
		return nil, fmt.Errorf("resetting ingestion progress: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM otlp_envelopes"); err != nil {
		return nil, fmt.Errorf("clearing OTLP envelopes: %w", err)
	}
	for table := range ingestStatements {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return nil, fmt.Errorf("clearing %s: %w", table, err)
		}
	}
	report := &IngestReport{Signals: map[string]*SignalReport{}}
	var cursorPaths []any
	for _, descriptor := range otlpSignals {
		signal := descriptor.name
		rep := &SignalReport{}
		report.Signals[signal] = rep
		path, err := filepath.Abs(filepath.Join(dataPath, signal, signal+".jsonl"))
		if err != nil {
			return nil, err
		}
		if err := ingestFileTx(tx, signal, path, rep); err != nil {
			return nil, fmt.Errorf("reingesting %s: %w", signal, err)
		}
		if rep.Files > 0 {
			cursorPaths = append(cursorPaths, path)
		}
	}
	deleteCursors := "DELETE FROM ingest_state"
	if len(cursorPaths) > 0 {
		deleteCursors += " WHERE path NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(cursorPaths)), ",") + ")"
	}
	if _, err := tx.Exec(deleteCursors, cursorPaths...); err != nil {
		return nil, fmt.Errorf("clearing obsolete ingestion progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return report, nil
}
