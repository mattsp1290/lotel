package storage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type IngestReport struct {
	Signals map[string]*SignalReport `json:"signals"`
}
type SignalReport struct {
	Files          int   `json:"files"`
	Lines          int   `json:"lines"`
	Records        int   `json:"records"`
	SkippedLines   int   `json:"skipped_lines"`
	SkippedRecords int   `json:"skipped_records"`
	Bytes          int64 `json:"bytes"`
}

// IngestAll incrementally ingests complete JSONL lines in per-file transactions.
func IngestAll(db *sql.DB, dataPath string) (*IngestReport, error) {
	report := &IngestReport{Signals: map[string]*SignalReport{}}
	for _, signal := range ingestSignals {
		rep := &SignalReport{}
		report.Signals[signal] = rep
		path, err := filepath.Abs(filepath.Join(dataPath, signal, signal+".jsonl"))
		if err != nil {
			return nil, err
		}
		if err := ingestFile(db, signal, path, rep); err != nil {
			return nil, fmt.Errorf("ingesting %s: %w", signal, err)
		}
	}
	return report, nil
}

func fileFingerprint(f *os.File, length int) (string, error) {
	data := make([]byte, length)
	if length > 0 {
		if _, err := f.ReadAt(data, 0); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

var ingestSignals = []string{"traces", "metrics", "logs"}

func ingestFile(db *sql.DB, signal, path string, rep *SignalReport) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ingestFileTx(tx, signal, path, rep); err != nil {
		return err
	}
	return tx.Commit()
}

func ingestFileTx(tx *sql.Tx, signal, path string, rep *SignalReport) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	var offset int64
	var previous string
	var previousLen int
	err = tx.QueryRow("SELECT byte_offset,fingerprint,fingerprint_len FROM ingest_state WHERE path=?", path).Scan(&offset, &previous, &previousLen)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		offset = 0
	} else {
		if offset > stat.Size() || int64(previousLen) > stat.Size() {
			offset = 0
		} else {
			fingerprint, err := fileFingerprint(f, previousLen)
			if err != nil {
				return err
			}
			if fingerprint != previous {
				offset = 0
			}
		}
	}
	length := int(min(stat.Size(), 4096))
	fingerprint, err := fileFingerprint(f, length)
	if err != nil {
		return err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(f, 1<<20)
	rep.Files++
	stmt, err := tx.Prepare(ingestStatements[signal])
	if err != nil {
		return err
	}
	defer stmt.Close()
	var consumed int64
	// Restrict this transaction to the file size observed above.
	remaining := stat.Size() - offset
	for remaining > 0 {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if int64(len(line)) > remaining {
			break
		}
		consumed += int64(len(line))
		remaining -= int64(len(line))
		rep.Lines++
		var envelope map[string]json.RawMessage
		root := map[string]string{"traces": "resourceSpans", "metrics": "resourceMetrics", "logs": "resourceLogs"}[signal]
		if json.Unmarshal(line, &envelope) != nil || envelope[root] == nil {
			rep.SkippedLines++
			continue
		}
		var records, skipped int
		switch signal {
		case "traces":
			records, skipped, err = ingestTraces(stmt, line)
		case "metrics":
			records, skipped, err = ingestMetrics(stmt, line)
		case "logs":
			records, skipped, err = ingestLogs(stmt, line)
		}
		if err != nil {
			return err
		}
		rep.Records += records
		if skipped < 0 {
			rep.SkippedLines++
		} else {
			rep.SkippedRecords += skipped
		}
	}
	_, err = tx.Exec("INSERT INTO ingest_state VALUES (?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(path) DO UPDATE SET byte_offset=excluded.byte_offset,fingerprint=excluded.fingerprint,fingerprint_len=excluded.fingerprint_len,updated_at=excluded.updated_at", path, offset+consumed, fingerprint, length)
	if err != nil {
		return err
	}
	rep.Bytes = consumed
	return nil
}

func ingestTraces(stmt *sql.Stmt, line []byte) (int, int, error) {
	records, skipped := 0, 0

	var batch struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []otlpAttr `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Scope     otlpScope `json:"scope"`
				SchemaURL string    `json:"schemaUrl"`
				Spans     []struct {
					TraceState   string          `json:"traceState"`
					Events       []otlpSpanEvent `json:"events"`
					Links        []otlpSpanLink  `json:"links"`
					TraceID      string          `json:"traceId"`
					SpanID       string          `json:"spanId"`
					ParentSpanID string          `json:"parentSpanId"`
					Name         string          `json:"name"`
					Kind         int             `json:"kind"`
					StartTime    otlpNano        `json:"startTimeUnixNano"`
					EndTime      otlpNano        `json:"endTimeUnixNano"`
					Status       struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					} `json:"status"`
					Attributes []otlpAttr `json:"attributes"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}

	if err := json.Unmarshal(bytes.TrimSpace(line), &batch); err != nil {
		return 0, -1, nil
	}

	for _, rs := range batch.ResourceSpans {
		svcName := extractServiceName(rs.Resource.Attributes)
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				startTime := span.StartTime.Time()
				endTime := span.EndTime.Time()
				durationNs := int64(0)
				if !startTime.IsZero() && !endTime.IsZero() {
					durationNs = endTime.Sub(startTime).Nanoseconds()
				}
				attrs, _ := json.Marshal(attrsToMap(span.Attributes))

				records++
				_, err := stmt.Exec(
					span.TraceID, span.SpanID, nullStr(span.ParentSpanID),
					span.Name, span.Kind,
					startTime, endTime, durationNs,
					span.Status.Code, svcName,
					string(attrs), startTime.Format("2006-01-02"),
					span.TraceState, span.Status.Message, jsonText(attrsToMap(rs.Resource.Attributes)), ss.Scope.Name, ss.Scope.Version, ss.SchemaURL, spanEventsJSON(span.Events), spanLinksJSON(span.Links),
				)
				if err != nil {
					return 0, 0, fmt.Errorf("inserting span: %w", err)
				}
			}
		}
	}
	return records, skipped, nil
}

func ingestMetrics(stmt *sql.Stmt, line []byte) (int, int, error) {
	records, skipped := 0, 0

	var batch struct {
		ResourceMetrics []struct {
			Resource struct {
				Attributes []otlpAttr `json:"attributes"`
			} `json:"resource"`
			ScopeMetrics []struct {
				Scope   otlpScope    `json:"scope"`
				Metrics []otlpMetric `json:"metrics"`
			} `json:"scopeMetrics"`
		} `json:"resourceMetrics"`
	}

	if err := json.Unmarshal(bytes.TrimSpace(line), &batch); err != nil {
		return 0, -1, nil
	}

	for _, rm := range batch.ResourceMetrics {
		svcName := extractServiceName(rm.Resource.Attributes)
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				for _, dp := range extractDataPoints(m) {
					attrs, _ := json.Marshal(attrsToMap(dp.attributes))
					records++
					_, err := stmt.Exec(
						m.Name, dp.metricType, dp.value,
						dp.timestamp, svcName,
						dp.temporality, dp.monotonic,
						m.Unit, string(attrs),
						dp.timestamp.Format("2006-01-02"),
						m.Description, dp.startTimestamp, dp.count, dp.sum, dp.min, dp.max, dp.explicitBounds, dp.bucketCounts, dp.expScale, dp.expZeroCount, dp.expPositive, dp.expNegative, dp.quantiles, jsonText(attrsToMap(rm.Resource.Attributes)), sm.Scope.Name, sm.Scope.Version,
					)
					if err != nil {
						return 0, 0, fmt.Errorf("inserting metric: %w", err)
					}
				}
			}
		}
	}
	return records, skipped, nil
}

func ingestLogs(stmt *sql.Stmt, line []byte) (int, int, error) {
	records, skipped := 0, 0

	var batch struct {
		ResourceLogs []struct {
			Resource struct {
				Attributes []otlpAttr `json:"attributes"`
			} `json:"resource"`
			ScopeLogs []struct {
				Scope      otlpScope `json:"scope"`
				LogRecords []struct {
					ObservedTimeUnixNano otlpNano   `json:"observedTimeUnixNano"`
					EventName            string     `json:"eventName"`
					Flags                int        `json:"flags"`
					TimeUnixNano         otlpNano   `json:"timeUnixNano"`
					SeverityText         string     `json:"severityText"`
					SeverityNumber       int        `json:"severityNumber"`
					Body                 otlpValue  `json:"body"`
					TraceID              string     `json:"traceId"`
					SpanID               string     `json:"spanId"`
					Attributes           []otlpAttr `json:"attributes"`
				} `json:"logRecords"`
			} `json:"scopeLogs"`
		} `json:"resourceLogs"`
	}

	if err := json.Unmarshal(bytes.TrimSpace(line), &batch); err != nil {
		return 0, -1, nil
	}

	for _, rl := range batch.ResourceLogs {
		svcName := extractServiceName(rl.Resource.Attributes)
		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				ts := lr.TimeUnixNano.Time()
				if ts.IsZero() {
					ts = lr.ObservedTimeUnixNano.Time()
				}
				if ts.IsZero() {
					skipped++
					continue
				}
				attrs, _ := json.Marshal(attrsToMap(lr.Attributes))
				records++
				_, err := stmt.Exec(
					ts, lr.SeverityText, lr.SeverityNumber,
					lr.Body.String(), svcName,
					nullStr(lr.TraceID), nullStr(lr.SpanID),
					string(attrs), ts.Format("2006-01-02"),
					lr.ObservedTimeUnixNano.Time(), lr.EventName, jsonText(lr.Body.Any()), lr.Flags, jsonText(attrsToMap(rl.Resource.Attributes)), sl.Scope.Name, sl.Scope.Version,
				)
				if err != nil {
					return 0, 0, fmt.Errorf("inserting log: %w", err)
				}
			}
		}
	}
	return records, skipped, nil
}

func extractServiceName(attrs []otlpAttr) string {
	for _, a := range attrs {
		if a.Key == "service.name" {
			return a.Value.String()
		}
	}
	return "unknown"
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func jsonText(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func spanEventsJSON(events []otlpSpanEvent) string {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{"time": e.TimeUnixNano.Time().Format(time.RFC3339Nano), "name": e.Name, "attributes": attrsToMap(e.Attributes)})
	}
	return jsonText(out)
}
func spanLinksJSON(links []otlpSpanLink) string {
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		out = append(out, map[string]any{"trace_id": l.TraceID, "span_id": l.SpanID, "trace_state": l.TraceState, "attributes": attrsToMap(l.Attributes)})
	}
	return jsonText(out)
}

var ingestStatements = map[string]string{
	"traces":  `INSERT INTO traces (trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date,trace_state,status_message,resource_attributes,scope_name,scope_version,schema_url,events,links) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,?,?,?,?,?,?,?,?)`,
	"metrics": `INSERT INTO metrics (metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, attributes, date,description,start_timestamp,"count","sum","min","max",explicit_bounds,bucket_counts,exp_scale,exp_zero_count,exp_positive,exp_negative,quantiles,resource_attributes,scope_name,scope_version) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
	"logs":    `INSERT INTO logs (timestamp, severity, severity_number, body, service_name, trace_id, span_id, attributes, date,observed_timestamp,event_name,body_json,flags,resource_attributes,scope_name,scope_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?,?,?,?,?,?,?,?)`,
}
