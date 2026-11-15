package storage

import (
	"context"
	"database/sql"
	"fmt"
)

type metricEnvelopeSize struct{ count, bytes int }

// DuckDB materializes query results before returning rows. Preflight metadata
// first, then issue one payload query capped by the same transaction snapshot.
func metricEnvelopePreflight(ctx context.Context, tx *sql.Tx) (metricEnvelopeSize, error) {
	rows, err := tx.QueryContext(ctx, `SELECT strlen(CAST(payload AS VARCHAR)) FROM otlp_envelopes WHERE signal='metrics' LIMIT ?`, metricEnvelopeLimit+1)
	if err != nil {
		return metricEnvelopeSize{}, fmt.Errorf("querying metric envelope metadata: %w", err)
	}
	defer rows.Close()
	var size metricEnvelopeSize
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return metricEnvelopeSize{}, err
		}
		var n int
		if err := rows.Scan(&n); err != nil {
			return size, err
		}
		size.count++
		if size.count > metricEnvelopeLimit {
			return size, fmt.Errorf("indeterminate: metric envelope scan limit exceeded")
		}
		if n < 0 || n > metricEnvelopeByteLimit {
			return size, fmt.Errorf("indeterminate: metric envelope size limit exceeded")
		}
		size.bytes += n
		if size.bytes > metricTotalByteLimit {
			return size, fmt.Errorf("indeterminate: metric scan byte limit exceeded")
		}
	}
	return size, rows.Err()
}

func visitMetricEnvelopes(ctx context.Context, tx *sql.Tx, expected metricEnvelopeSize, visit func([]byte) error) error {
	rows, err := tx.QueryContext(ctx, `SELECT CAST(payload AS VARCHAR) FROM otlp_envelopes WHERE signal='metrics'`)
	if err != nil {
		return fmt.Errorf("querying metric envelopes: %w", err)
	}
	defer rows.Close()
	var actual metricEnvelopeSize
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		actual.count++
		actual.bytes += len(raw)
		if actual.count > expected.count || actual.bytes > expected.bytes || len(raw) > metricEnvelopeByteLimit {
			return fmt.Errorf("indeterminate: metric envelope changed during scan")
		}
		if err := visit([]byte(raw)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("indeterminate: metric envelope changed during scan")
	}
	return nil
}
