package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpanStatsIngestedRecords(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := time.Unix(1700000000, 0).UTC()
	type entry struct {
		service, name string
		kind, status  int
		duration      *int64
	}
	value := func(n int64) *int64 { return &n }
	entries := []entry{
		{"app", "request", 1, 0, value(0)},
		{"app", "request", 1, 2, value(10)},
		{"app", "request", 1, 2, value(10)},
		{"app", "request", 1, 1, value(30)},
		{"app", "request", 1, 0, nil},
		{"app", "request", 1, 2, value(-10)},
		{"app", "request", 2, 0, value(100)},
		{"app", "request", 2, 2, value(200)},
		{"app", "request", 2, 0, value(300)},
		{"other", "request", 1, 2, value(200)},
		{"app", "missing-end", 1, 0, nil},
	}
	path := filepath.Join(dir, "data", "traces")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(path, "traces.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		span := map[string]any{"traceId": fmt.Sprintf("%032x", i+1), "spanId": fmt.Sprintf("%016x", i+1), "name": e.name, "kind": e.kind, "status": map[string]any{"code": e.status}, "startTimeUnixNano": fmt.Sprint(base.UnixNano())}
		if e.duration != nil {
			span["endTimeUnixNano"] = fmt.Sprint(base.UnixNano() + *e.duration)
		}
		payload := map[string]any{"resourceSpans": []any{map[string]any{"resource": map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": e.service}}}}, "scopeSpans": []any{map[string]any{"spans": []any{span}}}}}}
		if err := json.NewEncoder(file).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := IngestAll(db, filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	opts := SpanStatsOptions{Since: base, Until: base, Service: "app"}
	rows, err := QuerySpanStats(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows: %+v", rows)
	}
	empty, first, second := rows[0], rows[1], rows[2]
	if empty.Name != "missing-end" || empty.SpanCount != 1 || empty.DurationCount != 0 || empty.DurationExcluded != 1 || empty.P50DurationNs != nil || empty.P95DurationNs != nil {
		t.Fatalf("missing: %+v", empty)
	}
	if first.Name != "request" || first.Kind == nil || *first.Kind != 1 || first.SpanCount != 6 || first.ErrorCount != 3 || first.ErrorRate != 0.5 || first.DurationCount != 4 || first.DurationExcluded != 2 || first.P50DurationNs == nil || *first.P50DurationNs != 10 || first.P95DurationNs == nil || *first.P95DurationNs != 30 {
		t.Fatalf("first: %+v", first)
	}
	if second.Kind == nil || *second.Kind != 2 || second.SpanCount != 3 || second.ErrorCount != 1 || second.P50DurationNs == nil || *second.P50DurationNs != 200 || second.P95DurationNs == nil || *second.P95DurationNs != 300 {
		t.Fatalf("second: %+v", second)
	}
	opts.Limit = 1
	rows, err = QuerySpanStats(db, opts)
	if err != nil || len(rows) != 1 || rows[0].Name != "missing-end" {
		t.Fatalf("limit: %+v %v", rows, err)
	}
	opts.Limit = 0
	opts.Since = base.Add(time.Nanosecond)
	opts.Until = base.Add(time.Second)
	rows, err = QuerySpanStats(db, opts)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("fractional lower: %+v %v", rows, err)
	}
	opts.Since = base.Add(-time.Second)
	opts.Until = base.Add(-time.Nanosecond)
	rows, err = QuerySpanStats(db, opts)
	if err != nil || len(rows) != 0 {
		t.Fatalf("fractional upper: %+v %v", rows, err)
	}
}

func TestSpanStatsNullProjection(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Unix(1700000000, 0).UTC()
	_, err = db.Exec(`INSERT INTO traces (trace_id, span_id, name, start_time, end_time, duration_ns, status_code, service_name, date) VALUES ('t', 's', 'incomplete', ?, NULL, NULL, NULL, 'app', CURRENT_DATE)`, at)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := QuerySpanStats(db, SpanStatsOptions{Since: at, Until: at})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %+v %v", rows, err)
	}
	r := rows[0]
	if r.Kind != nil || r.SpanCount != 1 || r.ErrorCount != 0 || r.ErrorRate != 0 || r.DurationCount != 0 || r.DurationExcluded != 1 || r.P50DurationNs != nil || r.P95DurationNs != nil {
		t.Fatalf("row: %+v", r)
	}
	for _, opts := range []SpanStatsOptions{{}, {Since: at, Until: at, Limit: -1}, {Since: at, Until: at.Add(-time.Second)}} {
		if _, err := QuerySpanStats(nil, opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
}

func TestProjectedSpanWindowsAgree(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := time.Unix(1700000000, 0).UTC()
	actual := base.Add(200 * time.Nanosecond)
	path := filepath.Join(dir, "data", "traces")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"app"}}]},"scopeSpans":[{"spans":[{"traceId":"11111111111111111111111111111111","spanId":"1111111111111111","name":"request","startTimeUnixNano":"%d","endTimeUnixNano":"%d"}]}]}]}`+"\n", actual.UnixNano(), actual.Add(time.Microsecond).UnixNano())
	if err := os.WriteFile(filepath.Join(path, "traces.jsonl"), []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := IngestAll(db, filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{{"projected instant", base, true}, {"original instant", actual, false}} {
		t.Run(tc.name, func(t *testing.T) {
			latest, err := LatestSignal(db, SignalAssertion{Signal: "traces", Service: "app", Since: tc.at, Until: tc.at})
			if err != nil || (latest != nil) != tc.want {
				t.Fatalf("assertion latest = %v, err = %v", latest, err)
			}
			rows, err := QuerySpanStats(db, SpanStatsOptions{Service: "app", Since: tc.at, Until: tc.at})
			if err != nil || (len(rows) != 0) != tc.want {
				t.Fatalf("span statistics = %+v, err = %v", rows, err)
			}
		})
	}
}
