package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIngestAndQueryRoundtrip tests the full pipeline: JSONL → ingest → query → prune.
func TestIngestAndQueryRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	dataPath := filepath.Join(tmp, "data")
	writeTestJSONL(t, dataPath)

	// Ingest all signals.
	if err := IngestAll(db, dataPath); err != nil {
		t.Fatalf("IngestAll: %v", err)
	}

	// Verify traces.
	traces, err := QueryTraces(db, QueryOptions{Service: "test-uuid-svc"})
	if err != nil {
		t.Fatalf("QueryTraces: %v", err)
	}
	if len(traces) != 2 {
		t.Errorf("traces count = %d, want 2", len(traces))
	}
	// Check deterministic ordering.
	if len(traces) >= 2 {
		if !traces[0].StartTime.Before(traces[1].StartTime) && !traces[0].StartTime.Equal(traces[1].StartTime) {
			t.Error("traces not ordered by start_time ASC")
		}
	}

	// Verify metrics.
	metrics, err := QueryMetrics(db, QueryOptions{Service: "test-uuid-svc"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(metrics) != 1 {
		t.Errorf("metrics count = %d, want 1", len(metrics))
	}

	// Verify aggregation.
	agg, err := AggregateMetrics(db, QueryOptions{Service: "test-uuid-svc"}, "http_requests_total")
	if err != nil {
		t.Fatalf("AggregateMetrics: %v", err)
	}
	if agg.Count != 1 {
		t.Errorf("agg count = %d, want 1", agg.Count)
	}

	// Verify logs.
	logs, err := QueryLogs(db, QueryOptions{Service: "test-uuid-svc"})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("logs count = %d, want 1", len(logs))
	}

	// Verify service filter excludes non-matching.
	traces2, _ := QueryTraces(db, QueryOptions{Service: "nonexistent"})
	if len(traces2) != 0 {
		t.Errorf("expected 0 traces for nonexistent service, got %d", len(traces2))
	}

	// Verify time range filter.
	traces3, _ := QueryTraces(db, QueryOptions{
		Service: "test-uuid-svc",
		Since:   time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if len(traces3) != 0 {
		t.Errorf("expected 0 traces in future range, got %d", len(traces3))
	}

	// Verify limit.
	traces4, _ := QueryTraces(db, QueryOptions{Service: "test-uuid-svc", Limit: 1})
	if len(traces4) != 1 {
		t.Errorf("expected 1 trace with limit=1, got %d", len(traces4))
	}

	// Prune with cutoff in the past (before the test data) deletes nothing.
	reports, err := Prune(db, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), "", true)
	if err != nil {
		t.Fatalf("Prune dry run: %v", err)
	}
	for _, r := range reports {
		if r.Deleted != 0 {
			t.Errorf("dry run with old cutoff should delete 0, got %d for %s", r.Deleted, r.Signal)
		}
	}

	// Prune everything (future cutoff deletes all).
	reports, err = Prune(db, time.Now().Add(1000*time.Hour), "", false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// Verify deletion.
	tracesPost, _ := QueryTraces(db, QueryOptions{})
	metricsPost, _ := QueryMetrics(db, QueryOptions{})
	logsPost, _ := QueryLogs(db, QueryOptions{})
	if len(tracesPost) != 0 || len(metricsPost) != 0 || len(logsPost) != 0 {
		t.Errorf("after prune: traces=%d, metrics=%d, logs=%d; want all 0", len(tracesPost), len(metricsPost), len(logsPost))
	}
}

func TestIngestIdempotent(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	dataPath := filepath.Join(tmp, "data")
	writeTestJSONL(t, dataPath)

	// Ingest twice — should duplicate data (no dedup in current design).
	IngestAll(db, dataPath)
	IngestAll(db, dataPath)

	traces, _ := QueryTraces(db, QueryOptions{Service: "test-uuid-svc"})
	// We expect 4 traces (2x2) since we ingested twice with no dedup.
	if len(traces) != 4 {
		t.Errorf("after double ingest: traces=%d, want 4", len(traces))
	}
}

func TestQueryEmptyDB(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	traces, err := QueryTraces(db, QueryOptions{Service: "nobody"})
	if err != nil {
		t.Fatalf("QueryTraces on empty: %v", err)
	}
	if traces != nil {
		t.Errorf("expected nil traces, got %v", traces)
	}

	agg, err := AggregateMetrics(db, QueryOptions{}, "no_metric")
	if err != nil {
		t.Fatalf("AggregateMetrics on empty: %v", err)
	}
	if agg.Count != 0 {
		t.Errorf("expected count=0, got %d", agg.Count)
	}
	if agg.Avg != nil {
		t.Errorf("expected nil avg, got %v", agg.Avg)
	}
}

func TestAggregateMetricsTimeWindow(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	base := time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC)
	insertTestMetric(t, db, "cpu_usage", "test-svc", 10.0, base)
	insertTestMetric(t, db, "cpu_usage", "test-svc", 30.0, base.Add(1*time.Hour))
	insertTestMetric(t, db, "cpu_usage", "test-svc", 50.0, base.Add(2*time.Hour))
	insertTestMetric(t, db, "cpu_usage", "other-svc", 100.0, base.Add(1*time.Hour))

	// Full range, test-svc only.
	agg, err := AggregateMetrics(db, QueryOptions{Service: "test-svc"}, "cpu_usage")
	if err != nil {
		t.Fatal(err)
	}
	if agg.Count != 3 {
		t.Errorf("count = %d, want 3", agg.Count)
	}
	if *agg.Avg != 30 {
		t.Errorf("avg = %f, want 30", *agg.Avg)
	}
	if *agg.Min != 10 {
		t.Errorf("min = %f, want 10", *agg.Min)
	}
	if *agg.Max != 50 {
		t.Errorf("max = %f, want 50", *agg.Max)
	}

	// Narrowed time window.
	agg2, err := AggregateMetrics(db, QueryOptions{
		Service: "test-svc",
		Since:   base.Add(30 * time.Minute),
		Until:   base.Add(90 * time.Minute),
	}, "cpu_usage")
	if err != nil {
		t.Fatal(err)
	}
	if agg2.Count != 1 {
		t.Errorf("windowed count = %d, want 1", agg2.Count)
	}
}

func insertTestMetric(t *testing.T, db *sql.DB, name, svc string, value float64, ts time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO metrics (metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, "gauge", value, ts, svc, 0, false, "", "{}", ts.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insertTestMetric: %v", err)
	}
}

func writeTestJSONL(t *testing.T, dataPath string) {
	t.Helper()

	for _, sub := range []string{"traces", "metrics", "logs"} {
		os.MkdirAll(filepath.Join(dataPath, sub), 0o755)
	}

	// Traces with two spans.
	tracesJSONL := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-uuid-svc"}}]},"scopeSpans":[{"spans":[{"traceId":"aaaa","spanId":"1111","name":"GET /api","kind":2,"startTimeUnixNano":"1700000000000000000","endTimeUnixNano":"1700000000100000000","status":{"code":1},"attributes":[{"key":"http.method","value":{"stringValue":"GET"}}]},{"traceId":"aaaa","spanId":"2222","parentSpanId":"1111","name":"db_query","kind":3,"startTimeUnixNano":"1700000000010000000","endTimeUnixNano":"1700000000050000000","status":{"code":1},"attributes":[{"key":"db.system","value":{"stringValue":"postgresql"}}]}]}]}]}
`

	// Metrics with one sum datapoint.
	metricsJSONL := `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-uuid-svc"}}]},"scopeMetrics":[{"metrics":[{"name":"http_requests_total","unit":"1","sum":{"dataPoints":[{"timeUnixNano":"1700000000000000000","asInt":"100","attributes":[{"key":"method","value":{"stringValue":"GET"}}]}],"aggregationTemporality":2,"isMonotonic":true}}]}]}]}
`

	// Logs with one record.
	logsJSONL := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-uuid-svc"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1700000000000000000","severityText":"INFO","severityNumber":9,"body":{"stringValue":"request processed"},"attributes":[{"key":"request.id","value":{"stringValue":"req-123"}}]}]}]}]}
`

	os.WriteFile(filepath.Join(dataPath, "traces", "traces.jsonl"), []byte(tracesJSONL), 0o644)
	os.WriteFile(filepath.Join(dataPath, "metrics", "metrics.jsonl"), []byte(metricsJSONL), 0o644)
	os.WriteFile(filepath.Join(dataPath, "logs", "logs.jsonl"), []byte(logsJSONL), 0o644)
}
