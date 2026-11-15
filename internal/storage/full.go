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
	tables := []string{"otlp_envelopes"}
	for table := range ingestStatements {
		tables = append(tables, table)
	}
	for _, table := range tables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return nil, fmt.Errorf("clearing %s: %w", table, err)
		}
	}
	if _, err := tx.Exec("UPDATE ingest_state SET byte_offset=0, fingerprint='', fingerprint_len=0"); err != nil {
		return nil, fmt.Errorf("resetting ingestion progress: %w", err)
	}
	report := &IngestReport{Signals: map[string]*SignalReport{}}
	var activePaths []any
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
			activePaths = append(activePaths, path)
		}
	}
	clearObsolete := "DELETE FROM ingest_state"
	if len(activePaths) > 0 {
		clearObsolete += " WHERE path NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(activePaths)), ",") + ")"
	}
	if _, err := tx.Exec(clearObsolete, activePaths...); err != nil {
		return nil, fmt.Errorf("clearing obsolete ingestion progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return report, nil
}
