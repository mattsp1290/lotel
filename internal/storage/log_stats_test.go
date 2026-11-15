package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLogStatsSeverityAndFilters(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, level := range []any{0, 1, 4, 5, 8, 9, 12, 13, 16, 17, 20, 21, 24, nil, -1, 25} {
		_, err := db.Exec(`INSERT INTO logs (timestamp, severity_number, severity, body, service_name, date) VALUES (?, ?, 'ERROR', 'literal 100% _done', 'app', CURRENT_DATE)`, at, level)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`INSERT INTO logs (timestamp, severity_number, body, service_name, date) VALUES (?, 17, 'literal 100% _done', 'app', CURRENT_DATE), (?, 17, 'literal 100X Zdone', 'app', CURRENT_DATE), (?, 17, 'literal 100% _done', 'other', CURRENT_DATE)`, at, at, at)
	if err != nil {
		t.Fatal(err)
	}
	opts := LogStatsOptions{Since: at, Until: at, Service: "app"}
	rows, err := QueryLogStats(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []LogStats{{"app", "unspecified", 1}, {"app", "trace", 2}, {"app", "debug", 2}, {"app", "info", 2}, {"app", "warn", 2}, {"app", "error", 4}, {"app", "fatal", 2}, {"app", "invalid", 3}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %+v, want %+v", rows, want)
	}
	body := "100% _DONE"
	opts.MinSeverity, opts.BodyContains = 17, &body
	rows, err = QueryLogStats(db, opts)
	if err != nil || !reflect.DeepEqual(rows, []LogStats{{"app", "error", 3}, {"app", "fatal", 2}}) {
		t.Fatalf("filtered %+v: %v", rows, err)
	}
	opts.Limit = 1
	rows, err = QueryLogStats(db, opts)
	if err != nil || !reflect.DeepEqual(rows, []LogStats{{"app", "error", 3}}) {
		t.Fatalf("limit %+v: %v", rows, err)
	}
	opts.Limit, opts.Since, opts.Until = 0, at.Add(time.Nanosecond), at.Add(time.Second)
	rows, err = QueryLogStats(db, opts)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("fractional lower %+v: %v", rows, err)
	}
	opts.Since, opts.Until = at.Add(-time.Second), at.Add(-time.Nanosecond)
	rows, err = QueryLogStats(db, opts)
	if err != nil || len(rows) != 0 {
		t.Fatalf("fractional upper %+v: %v", rows, err)
	}
	for _, o := range []LogStatsOptions{{}, {Since: at, Until: at, Limit: -1}, {Since: at, Until: at, MinSeverity: 2}, {Since: at, Until: at, BodyContains: new(string)}} {
		if _, err := QueryLogStats(nil, o); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
}

func TestLogStatsIngestionFallbackAndStructuredBody(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Unix(1700000000, 0).UTC()
	path := filepath.Join(dir, "data", "logs")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"app"}}]},"scopeLogs":[{"logRecords":[{"observedTimeUnixNano":"%d","severityNumber":17,"body":{"kvlistValue":{"values":[{"key":"message","value":{"stringValue":"100%% _done"}}]}}},{"severityNumber":17,"body":{"stringValue":"undated"}}]}]}]}`+"\n", at.UnixNano())
	if err := os.WriteFile(filepath.Join(path, "logs.jsonl"), []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := IngestAll(db, filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	body := "100% _done"
	rows, err := QueryLogStats(db, LogStatsOptions{Since: at, Until: at, Service: "app", BodyContains: &body})
	if err != nil || !reflect.DeepEqual(rows, []LogStats{{"app", "error", 1}}) {
		t.Fatalf("ingested %+v: %v", rows, err)
	}
}

func TestLogStatsOrdersServiceGroupsBeforeLimit(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Unix(1700000000, 0).UTC()
	_, err = db.Exec(`INSERT INTO logs (timestamp, severity_number, severity, body, service_name, date) VALUES (?, 0, 'ERROR', 'a', '', CURRENT_DATE), (?, 17, 'INFO', 'a', 'unknown', CURRENT_DATE), (?, 9, '', 'a', 'app', CURRENT_DATE), (?, 9, '', 'a', 'app', CURRENT_DATE)`, at, at, at, at)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := QueryLogStats(db, LogStatsOptions{Since: at, Until: at, Limit: 2})
	want := []LogStats{{"", "unspecified", 1}, {"app", "info", 2}}
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("ordered groups %+v: %v", rows, err)
	}
}
