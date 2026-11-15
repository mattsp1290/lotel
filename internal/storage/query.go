package storage

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// QueryOptions contains common query parameters.
type AttrFilter struct{ Key, Value string }

type QueryOptions struct {
	SpanErrorsOnly bool
	MinDuration    *time.Duration
	Name           string
	TraceID        string
	EventName      string
	Attrs          []AttrFilter
	Service        string
	Since          time.Time
	Until          time.Time
	Limit          int
}

type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

// TraceResult represents a single span in query results.
type TraceResult struct {
	TraceState         string         `json:"trace_state,omitempty"`
	StatusMessage      string         `json:"status_message,omitempty"`
	ResourceAttributes map[string]any `json:"resource_attributes,omitempty"`
	ScopeName          string         `json:"scope_name,omitempty"`
	ScopeVersion       string         `json:"scope_version,omitempty"`
	SchemaURL          string         `json:"schema_url,omitempty"`
	Events             []SpanEvent    `json:"events,omitempty"`
	Links              []SpanLink     `json:"links,omitempty"`
	TraceID            string         `json:"trace_id"`
	SpanID             string         `json:"span_id"`
	ParentSpanID       string         `json:"parent_span_id,omitempty"`
	Name               string         `json:"name"`
	Kind               int            `json:"kind"`
	StartTime          time.Time      `json:"start_time"`
	EndTime            time.Time      `json:"end_time"`
	DurationNs         int64          `json:"duration_ns"`
	StatusCode         int            `json:"status_code"`
	ServiceName        string         `json:"service_name"`
	Attributes         map[string]any `json:"attributes,omitempty"`
}

// MetricResult represents a single metric data point.
type MetricResult struct {
	Description            string         `json:"description,omitempty"`
	StartTimestamp         *time.Time     `json:"start_timestamp,omitempty"`
	Count                  *int64         `json:"count,omitempty"`
	Sum                    *float64       `json:"sum,omitempty"`
	Min                    *float64       `json:"min,omitempty"`
	Max                    *float64       `json:"max,omitempty"`
	ExplicitBounds         []any          `json:"explicit_bounds,omitempty"`
	BucketCounts           []int64        `json:"bucket_counts,omitempty"`
	ExpScale               *int           `json:"exp_scale,omitempty"`
	ExpZeroCount           *int64         `json:"exp_zero_count,omitempty"`
	ExpPositive            *ExpBuckets    `json:"exp_positive,omitempty"`
	ExpNegative            *ExpBuckets    `json:"exp_negative,omitempty"`
	Quantiles              []Quantile     `json:"quantiles,omitempty"`
	ResourceAttributes     map[string]any `json:"resource_attributes,omitempty"`
	ScopeName              string         `json:"scope_name,omitempty"`
	ScopeVersion           string         `json:"scope_version,omitempty"`
	MetricName             string         `json:"metric_name"`
	MetricType             string         `json:"metric_type"`
	Value                  *float64       `json:"value"`
	Timestamp              time.Time      `json:"timestamp"`
	ServiceName            string         `json:"service_name"`
	AggregationTemporality int            `json:"aggregation_temporality,omitempty"`
	IsMonotonic            bool           `json:"is_monotonic,omitempty"`
	Unit                   string         `json:"unit,omitempty"`
	Attributes             map[string]any `json:"attributes,omitempty"`
}

// LogResult represents a single log record.
type LogResult struct {
	ObservedTimestamp  *time.Time     `json:"observed_timestamp,omitempty"`
	EventName          string         `json:"event_name,omitempty"`
	BodyValue          any            `json:"body_value,omitempty"`
	Flags              int            `json:"flags,omitempty"`
	ResourceAttributes map[string]any `json:"resource_attributes,omitempty"`
	ScopeName          string         `json:"scope_name,omitempty"`
	ScopeVersion       string         `json:"scope_version,omitempty"`
	Timestamp          time.Time      `json:"timestamp"`
	Severity           string         `json:"severity,omitempty"`
	SeverityNumber     int            `json:"severity_number,omitempty"`
	Body               string         `json:"body"`
	ServiceName        string         `json:"service_name"`
	TraceID            string         `json:"trace_id,omitempty"`
	SpanID             string         `json:"span_id,omitempty"`
	Attributes         map[string]any `json:"attributes,omitempty"`
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
func QueryTraces(db queryer, opts QueryOptions) ([]TraceResult, error) {
	if err := validateQueryOptions(opts); err != nil {
		return nil, err
	}
	if opts.MinDuration != nil && *opts.MinDuration < 0 {
		return nil, fmt.Errorf("minimum duration must be nonnegative")
	}
	query := `SELECT trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, CAST(attributes AS VARCHAR), CAST(json_object('trace_state',"trace_state",'status_message',"status_message",'resource_attributes',"resource_attributes",'scope_name',"scope_name",'scope_version',"scope_version",'schema_url',"schema_url",'events',"events",'links',"links") AS VARCHAR) FROM traces WHERE 1=1`
	args := buildWhere(&query, opts, "start_time")
	args = append(args, buildFilters(&query, opts, "name")...)
	if opts.SpanErrorsOnly {
		query += " AND status_code = ?"
		args = append(args, 2)
	}
	if opts.MinDuration != nil {
		query += " AND duration_ns >= ?"
		args = append(args, int64(*opts.MinDuration))
	}

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
		var metadata string
		err := rows.Scan(&r.TraceID, &r.SpanID, &parentSpanID, &r.Name, &r.Kind, &r.StartTime, &r.EndTime, &r.DurationNs, &r.StatusCode, &r.ServiceName, &attrsJSON, &metadata)
		if err != nil {
			return nil, fmt.Errorf("scanning trace row: %w", err)
		}
		if parentSpanID.Valid {
			r.ParentSpanID = parentSpanID.String
		}
		if attrsJSON.Valid {
			if err := decodeJSON([]byte(attrsJSON.String), &r.Attributes); err != nil {
				return nil, fmt.Errorf("decoding attributes: %w", err)
			}
		}
		if err := decodeJSON([]byte(metadata), &r); err != nil {
			return nil, fmt.Errorf("decoding metadata: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// QueryMetrics returns metrics matching the given options.
func QueryMetrics(db *sql.DB, opts QueryOptions) ([]MetricResult, error) {
	if err := validateQueryOptions(opts); err != nil {
		return nil, err
	}
	query := `SELECT metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, CAST(attributes AS VARCHAR), CAST(json_object('description',"description",'start_timestamp',strftime("start_timestamp", '%Y-%m-%dT%H:%M:%S.%fZ'),'count',"count",'sum',"sum",'min',"min",'max',"max",'explicit_bounds',"explicit_bounds",'bucket_counts',"bucket_counts",'exp_scale',"exp_scale",'exp_zero_count',"exp_zero_count",'exp_positive',"exp_positive",'exp_negative',"exp_negative",'quantiles',"quantiles",'resource_attributes',"resource_attributes",'scope_name',"scope_name",'scope_version',"scope_version") AS VARCHAR) FROM metrics WHERE 1=1`
	args := buildWhere(&query, opts, "timestamp")
	args = append(args, buildFilters(&query, opts, "metric_name")...)

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
		var metadata string
		err := rows.Scan(&r.MetricName, &r.MetricType, &r.Value, &r.Timestamp, &r.ServiceName, &temporality, &monotonic, &unit, &attrsJSON, &metadata)
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
			if err := decodeJSON([]byte(attrsJSON.String), &r.Attributes); err != nil {
				return nil, fmt.Errorf("decoding attributes: %w", err)
			}
		}
		if err := decodeJSON([]byte(metadata), &r); err != nil {
			return nil, fmt.Errorf("decoding metadata: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// QueryLogs returns logs matching the given options.
func QueryLogs(db queryer, opts QueryOptions) ([]LogResult, error) {
	if err := validateQueryOptions(opts); err != nil {
		return nil, err
	}
	query := `SELECT timestamp, severity, severity_number, body, service_name, trace_id, span_id, CAST(attributes AS VARCHAR), CAST(json_object('observed_timestamp',strftime("observed_timestamp", '%Y-%m-%dT%H:%M:%S.%fZ'),'event_name',"event_name",'body_value',body_json,'flags',"flags",'resource_attributes',"resource_attributes",'scope_name',"scope_name",'scope_version',"scope_version") AS VARCHAR) FROM logs WHERE 1=1`
	args := buildWhere(&query, opts, "timestamp")
	args = append(args, buildFilters(&query, opts, "")...)

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
		var metadata string
		err := rows.Scan(&r.Timestamp, &severity, &r.SeverityNumber, &r.Body, &r.ServiceName, &traceID, &spanID, &attrsJSON, &metadata)
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
			if err := decodeJSON([]byte(attrsJSON.String), &r.Attributes); err != nil {
				return nil, fmt.Errorf("decoding attributes: %w", err)
			}
		}
		if err := decodeJSON([]byte(metadata), &r); err != nil {
			return nil, fmt.Errorf("decoding metadata: %w", err)
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

type SpanEvent struct {
	Time       time.Time      `json:"time"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
}
type SpanLink struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	TraceState string         `json:"trace_state"`
	Attributes map[string]any `json:"attributes"`
}
type ExpBuckets struct {
	Offset       int     `json:"offset"`
	BucketCounts []int64 `json:"bucket_counts"`
}
type Quantile struct {
	Quantile any `json:"quantile"`
	Value    any `json:"value"`
}

func validateQueryOptions(opts QueryOptions) error {
	for _, attr := range opts.Attrs {
		if attr.Key == "" {
			return fmt.Errorf("attribute key cannot be empty")
		}
		for _, r := range attr.Key {
			if unicode.IsControl(r) {
				return fmt.Errorf("attribute key contains a control character")
			}
		}
	}
	return nil
}

func buildFilters(query *string, opts QueryOptions, nameColumn string) []any {
	var args []any
	if opts.Name != "" && nameColumn != "" {
		*query += " AND " + nameColumn + " = ?"
		args = append(args, opts.Name)
	}
	if opts.TraceID != "" {
		*query += " AND lower(trace_id) = ?"
		args = append(args, strings.ToLower(opts.TraceID))
	}
	if opts.EventName != "" {
		*query += " AND event_name = ?"
		args = append(args, opts.EventName)
	}
	for _, attr := range opts.Attrs {
		key := strings.ReplaceAll(strings.ReplaceAll(attr.Key, `\`, `\\`), `"`, `\"`)
		*query += " AND json_extract_string(attributes, ?) = ?"
		args = append(args, `$."`+key+`"`, attr.Value)
	}
	return args
}

// decodeJSON keeps integers exact in interface-valued telemetry fields.
func decodeJSON(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(dest)
}
