package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
)

// ReingestAll atomically rebuilds indexed telemetry from retained JSONL files.
// Rows removed by pruning return when their original JSONL remains available.
func ReingestAll(db *sql.DB, dataPath string) (*IngestReport, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, table := range append([]string{"ingest_state"}, ingestSignals...) {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return nil, fmt.Errorf("clearing %s: %w", table, err)
		}
	}
	report := &IngestReport{Signals: map[string]*SignalReport{}}
	for _, signal := range ingestSignals {
		rep := &SignalReport{}
		report.Signals[signal] = rep
		path, err := filepath.Abs(filepath.Join(dataPath, signal, signal+".jsonl"))
		if err != nil {
			return nil, err
		}
		if err := ingestFileTx(tx, signal, path, rep); err != nil {
			return nil, fmt.Errorf("reingesting %s: %w", signal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return report, nil
}
