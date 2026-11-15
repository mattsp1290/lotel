package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type PruneReport struct {
	Signal               string `json:"signal"`
	ServiceName          string `json:"service_name,omitempty"`
	Deleted              int64  `json:"deleted"`
	OTLPEnvelopesDeleted int64  `json:"otlp_envelopes_deleted"`
	Cutoff               string `json:"cutoff"`
}

func Prune(db *sql.DB, cutoff time.Time, service string, dryRun bool) ([]PruneReport, error) {
	return prune(db, cutoff, service, dryRun, false)
}

func PruneAll(db *sql.DB, cutoff time.Time, service string, dryRun bool) ([]PruneReport, error) {
	return prune(db, cutoff, service, dryRun, true)
}

func prune(db *sql.DB, cutoff time.Time, service string, dryRun, includeUndated bool) ([]PruneReport, error) {
	cutoffNano, err := queryNano(cutoff)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	reports := []PruneReport{}
	for _, signal := range otlpSignals {
		report := PruneReport{Signal: signal.name, ServiceName: service, Cutoff: cutoff.Format(time.RFC3339)}
		if signal.name != "profiles" {
			timeColumn := "timestamp"
			if signal.name == "traces" {
				timeColumn = "start_time"
			}
			where := fmt.Sprintf(" WHERE %s < ?", timeColumn)
			args := []any{cutoff}
			if includeUndated {
				where = " WHERE 1=1"
				args = nil
			}
			if service != "" {
				where += " AND service_name=?"
				args = append(args, service)
			}
			if err := tx.QueryRow("SELECT COUNT(*) FROM "+signal.name+where, args...).Scan(&report.Deleted); err != nil {
				return nil, err
			}
			if !dryRun {
				if _, err := tx.Exec("DELETE FROM "+signal.name+where, args...); err != nil {
					return nil, err
				}
			}
		}
		where := " WHERE signal=? AND last_nano<CAST(? AS UBIGINT)"
		args := []any{signal.name, cutoffNano}
		if includeUndated {
			where = " WHERE signal=?"
			args = []any{signal.name}
		}
		if service != "" {
			where += " AND service_name=?"
			args = append(args, service)
		}
		if err := tx.QueryRow("SELECT COUNT(*) FROM otlp_envelopes"+where, args...).Scan(&report.OTLPEnvelopesDeleted); err != nil {
			return nil, err
		}
		if !dryRun {
			if _, err := tx.Exec("DELETE FROM otlp_envelopes"+where, args...); err != nil {
				return nil, err
			}
		}
		reports = append(reports, report)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return reports, nil
}
