package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type SpanStatsOptions struct {
	Service string
	Since   time.Time
	Until   time.Time
	Limit   int
}

type SpanStats struct {
	ServiceName      string  `json:"service_name"`
	Name             string  `json:"name"`
	Kind             *int64  `json:"kind"`
	SpanCount        int64   `json:"span_count"`
	ErrorCount       int64   `json:"error_count"`
	ErrorRate        float64 `json:"error_rate"`
	DurationCount    int64   `json:"duration_count"`
	DurationExcluded int64   `json:"duration_excluded"`
	P50DurationNs    *int64  `json:"p50_duration_ns"`
	P95DurationNs    *int64  `json:"p95_duration_ns"`
}

func QuerySpanStats(db *sql.DB, opts SpanStatsOptions) ([]SpanStats, error) {
	if opts.Since.IsZero() || opts.Until.IsZero() || opts.Since.After(opts.Until) || opts.Limit < 0 {
		return nil, fmt.Errorf("invalid span statistics window or limit")
	}
	start := opts.Since.Truncate(time.Microsecond)
	if start.Before(opts.Since) {
		start = start.Add(time.Microsecond)
	}
	end := opts.Until.Truncate(time.Microsecond)
	query := `WITH matched AS (
		SELECT service_name, name, kind, status_code, duration_ns,
			start_time <> TIMESTAMP '0001-01-01 00:00:00' AND end_time <> TIMESTAMP '0001-01-01 00:00:00' AND duration_ns >= 0 AS valid_duration
		FROM traces WHERE start_time >= ? AND start_time <= ?`
	args := []any{start, end}
	if opts.Service != "" {
		query += " AND service_name = ?"
		args = append(args, opts.Service)
	}
	query += `) SELECT service_name, name, kind, count(*) AS span_count,
		count(*) FILTER (WHERE status_code = 2) AS error_count,
		count(*) FILTER (WHERE status_code = 2)::DOUBLE / count(*) AS error_rate,
		count(*) FILTER (WHERE valid_duration) AS duration_count,
		count(*) - count(*) FILTER (WHERE valid_duration) AS duration_excluded,
		quantile_disc(duration_ns, 0.5) FILTER (WHERE valid_duration) AS p50,
		quantile_disc(duration_ns, 0.95) FILTER (WHERE valid_duration) AS p95
		FROM matched GROUP BY service_name, name, kind ORDER BY service_name, name, kind`
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying span statistics: %w", err)
	}
	defer rows.Close()
	results := []SpanStats{}
	for rows.Next() {
		var result SpanStats
		var kind, p50, p95 sql.NullInt64
		if err := rows.Scan(&result.ServiceName, &result.Name, &kind, &result.SpanCount, &result.ErrorCount, &result.ErrorRate, &result.DurationCount, &result.DurationExcluded, &p50, &p95); err != nil {
			return nil, fmt.Errorf("scanning span statistics: %w", err)
		}
		if kind.Valid {
			result.Kind = &kind.Int64
		}
		if p50.Valid {
			result.P50DurationNs = &p50.Int64
		}
		if p95.Valid {
			result.P95DurationNs = &p95.Int64
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
