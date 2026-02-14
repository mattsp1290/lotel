package storage

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// IngestAll reads all JSONL files from dataPath and ingests into db.
func IngestAll(db *sql.DB, dataPath string) error {
	for _, signal := range []string{"traces", "metrics", "logs"} {
		file := filepath.Join(dataPath, signal, signal+".jsonl")
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue
		}
		switch signal {
		case "traces":
			if err := ingestTraces(db, file); err != nil {
				return fmt.Errorf("ingesting traces: %w", err)
			}
		case "metrics":
			if err := ingestMetrics(db, file); err != nil {
				return fmt.Errorf("ingesting metrics: %w", err)
			}
		case "logs":
			if err := ingestLogs(db, file); err != nil {
				return fmt.Errorf("ingesting logs: %w", err)
			}
		}
	}
	return nil
}

func ingestTraces(db *sql.DB, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO traces (trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var batch struct {
			ResourceSpans []struct {
				Resource struct {
					Attributes []otlpAttr `json:"attributes"`
				} `json:"resource"`
				ScopeSpans []struct {
					Spans []struct {
						TraceID      string     `json:"traceId"`
						SpanID       string     `json:"spanId"`
						ParentSpanID string     `json:"parentSpanId"`
						Name         string     `json:"name"`
						Kind         int        `json:"kind"`
						StartTime    otlpNano   `json:"startTimeUnixNano"`
						EndTime      otlpNano   `json:"endTimeUnixNano"`
						Status       struct {
							Code int `json:"code"`
						} `json:"status"`
						Attributes []otlpAttr `json:"attributes"`
					} `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &batch); err != nil {
			continue // skip malformed lines
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
					attrs, _ := json.Marshal(flattenAttrs(span.Attributes))

					_, err := stmt.Exec(
						span.TraceID, span.SpanID, nullStr(span.ParentSpanID),
						span.Name, span.Kind,
						startTime, endTime, durationNs,
						span.Status.Code, svcName,
						string(attrs), startTime.Format("2006-01-02"),
					)
					if err != nil {
						return fmt.Errorf("inserting span: %w", err)
					}
				}
			}
		}
	}
	return tx.Commit()
}

func ingestMetrics(db *sql.DB, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO metrics (metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var batch struct {
			ResourceMetrics []struct {
				Resource struct {
					Attributes []otlpAttr `json:"attributes"`
				} `json:"resource"`
				ScopeMetrics []struct {
					Metrics []otlpMetric `json:"metrics"`
				} `json:"scopeMetrics"`
			} `json:"resourceMetrics"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &batch); err != nil {
			continue
		}

		for _, rm := range batch.ResourceMetrics {
			svcName := extractServiceName(rm.Resource.Attributes)
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					for _, dp := range extractDataPoints(m) {
						attrs, _ := json.Marshal(flattenAttrs(dp.attributes))
						_, err := stmt.Exec(
							m.Name, dp.metricType, dp.value,
							dp.timestamp, svcName,
							dp.temporality, dp.monotonic,
							m.Unit, string(attrs),
							dp.timestamp.Format("2006-01-02"),
						)
						if err != nil {
							return fmt.Errorf("inserting metric: %w", err)
						}
					}
				}
			}
		}
	}
	return tx.Commit()
}

func ingestLogs(db *sql.DB, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO logs (timestamp, severity, severity_number, body, service_name, trace_id, span_id, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var batch struct {
			ResourceLogs []struct {
				Resource struct {
					Attributes []otlpAttr `json:"attributes"`
				} `json:"resource"`
				ScopeLogs []struct {
					LogRecords []struct {
						TimeUnixNano   otlpNano   `json:"timeUnixNano"`
						SeverityText   string     `json:"severityText"`
						SeverityNumber int        `json:"severityNumber"`
						Body           otlpValue  `json:"body"`
						TraceID        string     `json:"traceId"`
						SpanID         string     `json:"spanId"`
						Attributes     []otlpAttr `json:"attributes"`
					} `json:"logRecords"`
				} `json:"scopeLogs"`
			} `json:"resourceLogs"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &batch); err != nil {
			continue
		}

		for _, rl := range batch.ResourceLogs {
			svcName := extractServiceName(rl.Resource.Attributes)
			for _, sl := range rl.ScopeLogs {
				for _, lr := range sl.LogRecords {
					ts := lr.TimeUnixNano.Time()
					attrs, _ := json.Marshal(flattenAttrs(lr.Attributes))
					_, err := stmt.Exec(
						ts, lr.SeverityText, lr.SeverityNumber,
						lr.Body.String(), svcName,
						nullStr(lr.TraceID), nullStr(lr.SpanID),
						string(attrs), ts.Format("2006-01-02"),
					)
					if err != nil {
						return fmt.Errorf("inserting log: %w", err)
					}
				}
			}
		}
	}
	return tx.Commit()
}

// otlpAttr represents an OTLP key-value attribute.
type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

// otlpValue represents an OTLP typed value.
type otlpValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

func (v otlpValue) String() string {
	if v.StringValue != nil {
		return *v.StringValue
	}
	if v.IntValue != nil {
		return *v.IntValue
	}
	if v.BoolValue != nil {
		if *v.BoolValue {
			return "true"
		}
		return "false"
	}
	if v.DoubleValue != nil {
		return fmt.Sprintf("%g", *v.DoubleValue)
	}
	return ""
}

// otlpNano handles nanosecond timestamps that may be strings or integers.
type otlpNano int64

func (n *otlpNano) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		var v int64
		fmt.Sscanf(s, "%d", &v)
		*n = otlpNano(v)
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*n = otlpNano(v)
	return nil
}

func (n otlpNano) Time() time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(n)).UTC()
}

type otlpMetric struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Unit        string          `json:"unit"`
	Sum         *otlpSum        `json:"sum,omitempty"`
	Gauge       *otlpGauge      `json:"gauge,omitempty"`
	Histogram   *otlpHistogram  `json:"histogram,omitempty"`
}

type otlpSum struct {
	DataPoints             []otlpDataPoint `json:"dataPoints"`
	AggregationTemporality int             `json:"aggregationTemporality"`
	IsMonotonic            bool            `json:"isMonotonic"`
}

type otlpGauge struct {
	DataPoints []otlpDataPoint `json:"dataPoints"`
}

type otlpHistogram struct {
	DataPoints             []otlpHistogramDP `json:"dataPoints"`
	AggregationTemporality int               `json:"aggregationTemporality"`
}

type otlpDataPoint struct {
	Attributes   []otlpAttr `json:"attributes"`
	TimeUnixNano otlpNano   `json:"timeUnixNano"`
	AsInt        *string    `json:"asInt,omitempty"`
	AsDouble     *float64   `json:"asDouble,omitempty"`
}

func (dp otlpDataPoint) Value() float64 {
	if dp.AsDouble != nil {
		return *dp.AsDouble
	}
	if dp.AsInt != nil {
		var v float64
		fmt.Sscanf(*dp.AsInt, "%f", &v)
		return v
	}
	return 0
}

type otlpHistogramDP struct {
	Attributes   []otlpAttr `json:"attributes"`
	TimeUnixNano otlpNano   `json:"timeUnixNano"`
	Count        *string    `json:"count,omitempty"`
	Sum          *float64   `json:"sum,omitempty"`
}

type metricPoint struct {
	metricType string
	value      float64
	timestamp  time.Time
	temporality int
	monotonic   bool
	attributes  []otlpAttr
}

func extractDataPoints(m otlpMetric) []metricPoint {
	var points []metricPoint
	if m.Sum != nil {
		for _, dp := range m.Sum.DataPoints {
			points = append(points, metricPoint{
				metricType:  "sum",
				value:       dp.Value(),
				timestamp:   dp.TimeUnixNano.Time(),
				temporality: m.Sum.AggregationTemporality,
				monotonic:   m.Sum.IsMonotonic,
				attributes:  dp.Attributes,
			})
		}
	}
	if m.Gauge != nil {
		for _, dp := range m.Gauge.DataPoints {
			points = append(points, metricPoint{
				metricType: "gauge",
				value:      dp.Value(),
				timestamp:  dp.TimeUnixNano.Time(),
				attributes: dp.Attributes,
			})
		}
	}
	if m.Histogram != nil {
		for _, dp := range m.Histogram.DataPoints {
			v := 0.0
			if dp.Sum != nil {
				v = *dp.Sum
			}
			points = append(points, metricPoint{
				metricType:  "histogram",
				value:       v,
				timestamp:   dp.TimeUnixNano.Time(),
				temporality: m.Histogram.AggregationTemporality,
				attributes:  dp.Attributes,
			})
		}
	}
	return points
}

func extractServiceName(attrs []otlpAttr) string {
	for _, a := range attrs {
		if a.Key == "service.name" {
			return a.Value.String()
		}
	}
	return "unknown"
}

func flattenAttrs(attrs []otlpAttr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value.String()
	}
	return m
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
