package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// QueryOptions contains common query parameters.
type QueryOptions struct {
	Service string
	Since   time.Time
	Until   time.Time
	Limit   int
}

// TraceResult represents a single span in query results.
type TraceResult struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Kind         int               `json:"kind"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	DurationNs   int64             `json:"duration_ns"`
	StatusCode   int               `json:"status_code"`
	ServiceName  string            `json:"service_name"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

// MetricResult represents a single metric data point.
type MetricResult struct {
	MetricName             string            `json:"metric_name"`
	MetricType             string            `json:"metric_type"`
	Value                  float64           `json:"value"`
	Timestamp              time.Time         `json:"timestamp"`
	ServiceName            string            `json:"service_name"`
	AggregationTemporality int               `json:"aggregation_temporality,omitempty"`
	IsMonotonic            bool              `json:"is_monotonic,omitempty"`
	Unit                   string            `json:"unit,omitempty"`
	Attributes             map[string]string `json:"attributes,omitempty"`
}

// LogResult represents a single log record.
type LogResult struct {
	Timestamp      time.Time         `json:"timestamp"`
	Severity       string            `json:"severity,omitempty"`
	SeverityNumber int               `json:"severity_number,omitempty"`
	Body           string            `json:"body"`
	ServiceName    string            `json:"service_name"`
	TraceID        string            `json:"trace_id,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// MetricAggregation holds aggregation results for a metric.
type MetricAggregation struct {
	MetricName  string   `json:"metric_name"`
	ServiceName string   `json:"service_name"`
	Count       int      `json:"count"`
	Avg         *float64 `json:"avg,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
}

// QueryTraces returns traces matching the given options.
func QueryTraces(db *sql.DB, opts QueryOptions) ([]TraceResult, error) {
	query := `SELECT trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, CAST(attributes AS VARCHAR) FROM traces WHERE 1=1`
	args := buildWhere(&query, opts, "start_time")

	query += " ORDER BY start_time ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying traces: %w", err)
	}
	defer rows.Close()

	var results []TraceResult
	for rows.Next() {
		var r TraceResult
		var parentSpanID sql.NullString
		var attrsJSON sql.NullString
		err := rows.Scan(&r.TraceID, &r.SpanID, &parentSpanID, &r.Name, &r.Kind, &r.StartTime, &r.EndTime, &r.DurationNs, &r.StatusCode, &r.ServiceName, &attrsJSON)
		if err != nil {
			return nil, fmt.Errorf("scanning trace row: %w", err)
		}
		if parentSpanID.Valid {
			r.ParentSpanID = parentSpanID.String
		}
		if attrsJSON.Valid {
			json.Unmarshal([]byte(attrsJSON.String), &r.Attributes)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// QueryMetrics returns metrics matching the given options.
func QueryMetrics(db *sql.DB, opts QueryOptions) ([]MetricResult, error) {
	query := `SELECT metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, CAST(attributes AS VARCHAR) FROM metrics WHERE 1=1`
	args := buildWhere(&query, opts, "timestamp")

	query += " ORDER BY timestamp ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	defer rows.Close()

	var results []MetricResult
	for rows.Next() {
		var r MetricResult
		var temporality sql.NullInt64
		var monotonic sql.NullBool
		var unit sql.NullString
		var attrsJSON sql.NullString
		err := rows.Scan(&r.MetricName, &r.MetricType, &r.Value, &r.Timestamp, &r.ServiceName, &temporality, &monotonic, &unit, &attrsJSON)
		if err != nil {
			return nil, fmt.Errorf("scanning metric row: %w", err)
		}
		if temporality.Valid {
			r.AggregationTemporality = int(temporality.Int64)
		}
		if monotonic.Valid {
			r.IsMonotonic = monotonic.Bool
		}
		if unit.Valid {
			r.Unit = unit.String
		}
		if attrsJSON.Valid {
			json.Unmarshal([]byte(attrsJSON.String), &r.Attributes)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// QueryLogs returns logs matching the given options.
func QueryLogs(db *sql.DB, opts QueryOptions) ([]LogResult, error) {
	query := `SELECT timestamp, severity, severity_number, body, service_name, trace_id, span_id, CAST(attributes AS VARCHAR) FROM logs WHERE 1=1`
	args := buildWhere(&query, opts, "timestamp")

	query += " ORDER BY timestamp ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var results []LogResult
	for rows.Next() {
		var r LogResult
		var severity sql.NullString
		var traceID, spanID sql.NullString
		var attrsJSON sql.NullString
		err := rows.Scan(&r.Timestamp, &severity, &r.SeverityNumber, &r.Body, &r.ServiceName, &traceID, &spanID, &attrsJSON)
		if err != nil {
			return nil, fmt.Errorf("scanning log row: %w", err)
		}
		if severity.Valid {
			r.Severity = severity.String
		}
		if traceID.Valid {
			r.TraceID = traceID.String
		}
		if spanID.Valid {
			r.SpanID = spanID.String
		}
		if attrsJSON.Valid {
			json.Unmarshal([]byte(attrsJSON.String), &r.Attributes)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// AggregateMetrics computes avg/min/max for metrics matching the given options.
func AggregateMetrics(db *sql.DB, opts QueryOptions, metricName string) (*MetricAggregation, error) {
	query := `SELECT COUNT(*), AVG(value), MIN(value), MAX(value) FROM metrics WHERE metric_name = ?`
	args := []interface{}{metricName}

	if opts.Service != "" {
		query += " AND service_name = ?"
		args = append(args, opts.Service)
	}
	if !opts.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, opts.Since)
	}
	if !opts.Until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, opts.Until)
	}

	var count int
	var avg, min, max sql.NullFloat64
	err := db.QueryRow(query, args...).Scan(&count, &avg, &min, &max)
	if err != nil {
		return nil, fmt.Errorf("aggregating metrics: %w", err)
	}

	result := &MetricAggregation{
		MetricName:  metricName,
		ServiceName: opts.Service,
		Count:       count,
	}
	if avg.Valid {
		result.Avg = &avg.Float64
	}
	if min.Valid {
		result.Min = &min.Float64
	}
	if max.Valid {
		result.Max = &max.Float64
	}
	return result, nil
}

func buildWhere(query *string, opts QueryOptions, timeCol string) []interface{} {
	var args []interface{}
	if opts.Service != "" {
		*query += " AND service_name = ?"
		args = append(args, opts.Service)
	}
	if !opts.Since.IsZero() {
		*query += fmt.Sprintf(" AND %s >= ?", timeCol)
		args = append(args, opts.Since)
	}
	if !opts.Until.IsZero() {
		*query += fmt.Sprintf(" AND %s <= ?", timeCol)
		args = append(args, opts.Until)
	}
	return args
}
