package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// PruneReport describes what was or would be pruned.
type PruneReport struct {
	Signal      string `json:"signal"`
	ServiceName string `json:"service_name,omitempty"`
	Deleted     int64  `json:"deleted"`
	Cutoff      string `json:"cutoff"`
}

// Prune deletes telemetry data older than the cutoff time.
// If dryRun is true, returns what would be deleted without deleting.
func Prune(db *sql.DB, cutoff time.Time, service string, dryRun bool) ([]PruneReport, error) {
	signals := []string{"traces", "metrics", "logs"}
	timeCols := map[string]string{
		"traces":  "start_time",
		"metrics": "timestamp",
		"logs":    "timestamp",
	}

	var reports []PruneReport
	for _, signal := range signals {
		timeCol := timeCols[signal]

		// Count rows that would be deleted.
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s < ?", signal, timeCol)
		args := []interface{}{cutoff}
		if service != "" {
			countQuery += " AND service_name = ?"
			args = append(args, service)
		}

		var count int64
		if err := db.QueryRow(countQuery, args...).Scan(&count); err != nil {
			return nil, fmt.Errorf("counting %s for prune: %w", signal, err)
		}

		if !dryRun && count > 0 {
			deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE %s < ?", signal, timeCol)
			deleteArgs := []interface{}{cutoff}
			if service != "" {
				deleteQuery += " AND service_name = ?"
				deleteArgs = append(deleteArgs, service)
			}
			result, err := db.Exec(deleteQuery, deleteArgs...)
			if err != nil {
				return nil, fmt.Errorf("pruning %s: %w", signal, err)
			}
			count, _ = result.RowsAffected()
		}

		reports = append(reports, PruneReport{
			Signal:      signal,
			ServiceName: service,
			Deleted:     count,
			Cutoff:      cutoff.Format(time.RFC3339),
		})
	}
	return reports, nil
}
