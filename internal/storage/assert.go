package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type SignalAssertion struct {
	Signal  string
	Service string
	Since   time.Time
	Until   time.Time
}

func LatestSignal(db *sql.DB, opts SignalAssertion) (*time.Time, error) {
	var table, column string
	switch opts.Signal {
	case "traces":
		table, column = "traces", "start_time"
	case "metrics":
		table, column = "metrics", "timestamp"
	case "logs":
		table, column = "logs", "timestamp"
	default:
		return nil, fmt.Errorf("unsupported signal %q", opts.Signal)
	}
	if opts.Service == "" || opts.Since.IsZero() || opts.Until.IsZero() || opts.Since.After(opts.Until) {
		return nil, fmt.Errorf("invalid signal assertion window or service")
	}
	query := fmt.Sprintf("SELECT MAX(%s) FROM %s WHERE service_name = ? AND %s >= ? AND %s <= ?", column, table, column, column)
	start := opts.Since.Truncate(time.Microsecond)
	if start.Before(opts.Since) {
		start = start.Add(time.Microsecond)
	}
	end := opts.Until.Truncate(time.Microsecond)
	var latest sql.NullTime
	if err := db.QueryRow(query, opts.Service, start, end).Scan(&latest); err != nil {
		return nil, fmt.Errorf("querying signal evidence: %w", err)
	}
	if !latest.Valid {
		return nil, nil
	}
	t := latest.Time.UTC()
	return &t, nil
}
