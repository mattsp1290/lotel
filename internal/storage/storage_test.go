package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) (*os.File, func()) {
	t.Helper()
	tmp := t.TempDir()
	dbFile := filepath.Join(tmp, "test.db")
	f, _ := os.Create(dbFile)
	return f, func() { f.Close() }
}

func TestMigrateAndQuery(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Insert a trace directly.
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = db.Exec(`INSERT INTO traces (trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"abc123", "span1", nil, "GET /test", 2, now, now.Add(time.Millisecond), 1000000, 1, "test-svc", `{"http.method":"GET"}`, now.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("insert trace: %v", err)
	}

	// Query traces.
	results, err := QueryTraces(db, QueryOptions{Service: "test-svc"})
	if err != nil {
		t.Fatalf("QueryTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(results))
	}
	if results[0].TraceID != "abc123" {
		t.Errorf("trace_id = %q, want abc123", results[0].TraceID)
	}
	if results[0].ServiceName != "test-svc" {
		t.Errorf("service_name = %q, want test-svc", results[0].ServiceName)
	}
	if results[0].Attributes["http.method"] != "GET" {
		t.Errorf("attributes[http.method] = %q, want GET", results[0].Attributes["http.method"])
	}

	// Query with wrong service returns nothing.
	results, err = QueryTraces(db, QueryOptions{Service: "other-svc"})
	if err != nil {
		t.Fatalf("QueryTraces other: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 traces for other-svc, got %d", len(results))
	}
}

func TestMetricsInsertAndAggregate(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Microsecond)
	for i, v := range []float64{10, 20, 30} {
		ts := now.Add(time.Duration(i) * time.Minute)
		_, err := db.Exec(`INSERT INTO metrics (metric_name, metric_type, value, timestamp, service_name, aggregation_temporality, is_monotonic, unit, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"request_count", "sum", v, ts, "test-svc", 2, true, "1", `{}`, ts.Format("2006-01-02"))
		if err != nil {
			t.Fatalf("insert metric %d: %v", i, err)
		}
	}

	agg, err := AggregateMetrics(db, QueryOptions{Service: "test-svc"}, "request_count")
	if err != nil {
		t.Fatalf("AggregateMetrics: %v", err)
	}
	if agg.Count != 3 {
		t.Errorf("count = %d, want 3", agg.Count)
	}
	if agg.Avg == nil || *agg.Avg != 20 {
		t.Errorf("avg = %v, want 20", agg.Avg)
	}
	if agg.Min == nil || *agg.Min != 10 {
		t.Errorf("min = %v, want 10", agg.Min)
	}
	if agg.Max == nil || *agg.Max != 30 {
		t.Errorf("max = %v, want 30", agg.Max)
	}
}

func TestPrune(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-48 * time.Hour)

	// Insert an old and a new trace.
	for _, ts := range []time.Time{old, now} {
		_, err := db.Exec(`INSERT INTO traces (trace_id, span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"t-"+ts.Format("150405"), "s1", "GET /", 2, ts, ts.Add(time.Millisecond), 1000000, 1, "test-svc", `{}`, ts.Format("2006-01-02"))
		if err != nil {
			t.Fatalf("insert trace: %v", err)
		}
	}

	// Dry run.
	cutoff := now.Add(-24 * time.Hour)
	reports, err := Prune(db, cutoff, "", true)
	if err != nil {
		t.Fatalf("Prune dry run: %v", err)
	}
	for _, r := range reports {
		if r.Signal == "traces" && r.Deleted != 1 {
			t.Errorf("dry run traces deleted = %d, want 1", r.Deleted)
		}
	}

	// Verify nothing was actually deleted.
	traces, _ := QueryTraces(db, QueryOptions{})
	if len(traces) != 2 {
		t.Errorf("after dry run, traces = %d, want 2", len(traces))
	}

	// Actual prune.
	reports, err = Prune(db, cutoff, "", false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, r := range reports {
		if r.Signal == "traces" && r.Deleted != 1 {
			t.Errorf("prune traces deleted = %d, want 1", r.Deleted)
		}
	}

	// Verify only new trace remains.
	traces, _ = QueryTraces(db, QueryOptions{})
	if len(traces) != 1 {
		t.Errorf("after prune, traces = %d, want 1", len(traces))
	}
}

func TestIngestTraces(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Create a JSONL file.
	tracesDir := filepath.Join(tmp, "data", "traces")
	os.MkdirAll(tracesDir, 0o755)
	jsonl := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"my-app"}}]},"scopeSpans":[{"spans":[{"traceId":"aaa","spanId":"bbb","name":"GET /hello","kind":2,"startTimeUnixNano":"1700000000000000000","endTimeUnixNano":"1700000001000000000","status":{"code":1},"attributes":[{"key":"http.method","value":{"stringValue":"GET"}}]}]}]}]}
`
	os.WriteFile(filepath.Join(tracesDir, "traces.jsonl"), []byte(jsonl), 0o644)

	// Ingest.
	if err := IngestAll(db, filepath.Join(tmp, "data")); err != nil {
		t.Fatalf("IngestAll: %v", err)
	}

	// Query.
	results, err := QueryTraces(db, QueryOptions{Service: "my-app"})
	if err != nil {
		t.Fatalf("QueryTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(results))
	}
	if results[0].Name != "GET /hello" {
		t.Errorf("name = %q, want GET /hello", results[0].Name)
	}
}

func TestIngestMetrics(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	metricsDir := filepath.Join(tmp, "data", "metrics")
	os.MkdirAll(metricsDir, 0o755)
	jsonl := `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"my-app"}}]},"scopeMetrics":[{"metrics":[{"name":"req_total","unit":"1","sum":{"dataPoints":[{"timeUnixNano":"1700000000000000000","asInt":"42","attributes":[]}],"aggregationTemporality":2,"isMonotonic":true}}]}]}]}
`
	os.WriteFile(filepath.Join(metricsDir, "metrics.jsonl"), []byte(jsonl), 0o644)

	if err := IngestAll(db, filepath.Join(tmp, "data")); err != nil {
		t.Fatalf("IngestAll: %v", err)
	}

	results, err := QueryMetrics(db, QueryOptions{Service: "my-app"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(results))
	}
	if results[0].Value != 42 {
		t.Errorf("value = %f, want 42", results[0].Value)
	}
}

func TestIngestLogs(t *testing.T) {
	tmp := t.TempDir()
	db, err := OpenDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	logsDir := filepath.Join(tmp, "data", "logs")
	os.MkdirAll(logsDir, 0o755)
	jsonl := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"my-app"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1700000000000000000","severityText":"INFO","severityNumber":9,"body":{"stringValue":"hello world"},"attributes":[]}]}]}]}
`
	os.WriteFile(filepath.Join(logsDir, "logs.jsonl"), []byte(jsonl), 0o644)

	if err := IngestAll(db, filepath.Join(tmp, "data")); err != nil {
		t.Fatalf("IngestAll: %v", err)
	}

	results, err := QueryLogs(db, QueryOptions{Service: "my-app"})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 log, got %d", len(results))
	}
	if results[0].Body != "hello world" {
		t.Errorf("body = %q, want hello world", results[0].Body)
	}
}
