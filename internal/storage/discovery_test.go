package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDiscoveryFromIngestedTelemetry(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	data := filepath.Join(dir, "data")
	base := time.Unix(1700000000, 0).UTC()
	str := func(s string) *string { return &s }
	type entry struct {
		signal  string
		service *string
		name    string
		kind    int
		offset  int
	}
	entries := []entry{
		{"traces", str("alpha"), "request", 1, 0},
		{"traces", str("alpha"), "request", 1, 0},
		{"traces", str("alpha"), "request", 2, 1},
		{"logs", str("alpha"), "", 0, 1},
		{"metrics", str("alpha"), "", 0, 1},
		{"logs", str("log-only"), "", 0, 1},
		{"metrics", str("metric-only"), "", 0, 1},
		{"traces", str("trace-only"), "work", 1, 2},
		{"traces", nil, "unnamed", 1, 1},
		{"logs", str(""), "", 0, 1},
	}
	for i, e := range entries {
		resource := map[string]any{}
		if e.service != nil {
			resource["attributes"] = []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": *e.service}}}
		}
		ts := fmt.Sprint(base.Add(time.Duration(e.offset) * time.Second).UnixNano())
		var payload any
		switch e.signal {
		case "traces":
			span := map[string]any{"traceId": fmt.Sprintf("%032x", i+1), "spanId": fmt.Sprintf("%016x", i+1), "name": e.name, "kind": e.kind, "startTimeUnixNano": ts, "endTimeUnixNano": ts}
			payload = map[string]any{"resourceSpans": []any{map[string]any{"resource": resource, "scopeSpans": []any{map[string]any{"spans": []any{span}}}}}}
		case "metrics":
			metric := map[string]any{"name": "count", "gauge": map[string]any{"dataPoints": []any{map[string]any{"timeUnixNano": ts, "asInt": "1"}}}}
			payload = map[string]any{"resourceMetrics": []any{map[string]any{"resource": resource, "scopeMetrics": []any{map[string]any{"metrics": []any{metric}}}}}}
		case "logs":
			log := map[string]any{"timeUnixNano": ts, "body": map[string]any{"stringValue": "message"}}
			payload = map[string]any{"resourceLogs": []any{map[string]any{"resource": resource, "scopeLogs": []any{map[string]any{"logRecords": []any{log}}}}}}
		}
		path := filepath.Join(data, e.signal)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		line, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(path, e.signal+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := IngestAll(db, data); err != nil {
		t.Fatal(err)
	}
	services, err := QueryServices(db, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []ServiceResult{{"", []string{"logs"}}, {"alpha", []string{"logs", "metrics", "traces"}}, {"log-only", []string{"logs"}}, {"metric-only", []string{"metrics"}}, {"trace-only", []string{"traces"}}, {"unknown", []string{"traces"}}}
	if !reflect.DeepEqual(services, want) {
		t.Fatalf("services got %#v, want %#v", services, want)
	}
	window := DiscoveryOptions{Since: base.Add(time.Second), Until: base.Add(time.Second), Limit: 2}
	services, err = QueryServices(db, window)
	if err != nil {
		t.Fatal(err)
	}
	want = []ServiceResult{{"", []string{"logs"}}, {"alpha", []string{"logs", "metrics", "traces"}}}
	if !reflect.DeepEqual(services, want) {
		t.Fatalf("window services got %#v, want %#v", services, want)
	}
	ops, err := QueryOperations(db, DiscoveryOptions{Service: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	wantOps := []OperationResult{{"alpha", "request", 1}, {"alpha", "request", 2}}
	if !reflect.DeepEqual(ops, wantOps) {
		t.Fatalf("operations got %#v, want %#v", ops, wantOps)
	}
	ops, err = QueryOperations(db, DiscoveryOptions{Since: base.Add(time.Second), Until: base.Add(time.Second), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ops, []OperationResult{{"alpha", "request", 2}}) {
		t.Fatalf("window operations got %#v", ops)
	}
	services, err = QueryServices(db, DiscoveryOptions{Service: "none"})
	if err != nil || len(services) != 0 || services == nil {
		t.Fatalf("empty services %#v, %v", services, err)
	}
	ops, err = QueryOperations(db, DiscoveryOptions{Service: "none"})
	if err != nil || len(ops) != 0 || ops == nil {
		t.Fatalf("empty operations %#v, %v", ops, err)
	}
}

func TestDiscoveryRejectsInvalidBounds(t *testing.T) {
	now := time.Now()
	for _, opts := range []DiscoveryOptions{
		{Limit: -1},
		{Since: now, Until: now.Add(-time.Second)},
	} {
		if _, err := QueryServices(nil, opts); err == nil {
			t.Errorf("services accepted %+v", opts)
		}
		if _, err := QueryOperations(nil, opts); err == nil {
			t.Errorf("operations accepted %+v", opts)
		}
	}
}
