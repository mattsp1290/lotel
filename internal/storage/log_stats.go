package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type LogStatsOptions struct {
	Service      string
	Since, Until time.Time
	Limit        int
	MinSeverity  int
	BodyContains *string
}

type LogStats struct {
	ServiceName string `json:"service_name"`
	Severity    string `json:"severity"`
	LogCount    int64  `json:"log_count"`
}

func QueryLogStats(db *sql.DB, opts LogStatsOptions) ([]LogStats, error) {
	if opts.Since.IsZero() || opts.Until.IsZero() || opts.Since.After(opts.Until) || opts.Limit < 0 {
		return nil, fmt.Errorf("invalid log statistics window or limit")
	}
	if err := validateLogFilters(opts.MinSeverity, opts.BodyContains); err != nil {
		return nil, err
	}
	start, end := projectedWindow(opts.Since, opts.Until)
	query := `WITH matched AS (
		SELECT service_name, CASE
			WHEN severity_number = 0 THEN 0
			WHEN severity_number BETWEEN 1 AND 4 THEN 1
			WHEN severity_number BETWEEN 5 AND 8 THEN 2
			WHEN severity_number BETWEEN 9 AND 12 THEN 3
			WHEN severity_number BETWEEN 13 AND 16 THEN 4
			WHEN severity_number BETWEEN 17 AND 20 THEN 5
			WHEN severity_number BETWEEN 21 AND 24 THEN 6
			ELSE 7 END AS band
		FROM logs WHERE timestamp >= ? AND timestamp <= ?`
	args := []any{start, end}
	if opts.Service != "" {
		query += " AND service_name = ?"
		args = append(args, opts.Service)
	}
	args = append(args, appendLogFilters(&query, opts.MinSeverity, opts.BodyContains)...)
	query += `) SELECT service_name, band, count(*) FROM matched
		GROUP BY service_name, band ORDER BY service_name, band`
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying log statistics: %w", err)
	}
	defer rows.Close()
	bands := [...]string{"unspecified", "trace", "debug", "info", "warn", "error", "fatal", "invalid"}
	results := []LogStats{}
	for rows.Next() {
		var row LogStats
		var band int
		if err := rows.Scan(&row.ServiceName, &band, &row.LogCount); err != nil {
			return nil, fmt.Errorf("scanning log statistics: %w", err)
		}
		row.Severity = bands[band]
		results = append(results, row)
	}
	return results, rows.Err()
}
