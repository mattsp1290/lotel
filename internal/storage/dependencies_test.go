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

func TestDependenciesFromIngestedSpans(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	type span struct {
		trace, id, parent, service string
		kind                       int
		offset                     int
	}
	tid := func(n int) string { return fmt.Sprintf("%032x", n) }
	sid := func(n int) string { return fmt.Sprintf("%016x", n) }
	spans := []span{
		{tid(1), sid(10), "", "alpha", 3, -1},
		{tid(1), sid(11), sid(10), "beta", 2, 0},
		{tid(2), sid(10), "", "wrong-trace", 3, -1},
		{tid(3), sid(20), "", "alpha", 3, 0}, {tid(3), sid(20), "", "alpha", 3, 0},
		{tid(3), sid(21), sid(20), "beta", 2, 0},
		{tid(4), sid(30), "", "alpha", 3, 0},
		{tid(4), sid(31), sid(30), "beta", 2, 0}, {tid(4), sid(31), sid(30), "beta", 2, 0},
		{tid(5), sid(40), sid(41), "beta", 2, 0},
		{tid(6), sid(50), "bad", "beta", 2, 0},
		{tid(7), sid(60), "", "unknown", 3, 0}, {tid(7), sid(61), sid(60), "beta", 2, 0},
		{tid(8), sid(70), "", "alpha", 1, 0}, {tid(8), sid(71), sid(70), "beta", 2, 0},
		{tid(9), sid(80), "", "alpha", 3, 0}, {tid(9), sid(81), sid(80), "alpha", 2, 0},
		{tid(10), sid(90), "", "alpha", 4, 0}, {tid(10), sid(91), sid(90), "beta", 5, 0},
		{tid(11), sid(100), "", "alpha", 3, 0}, {tid(11), sid(101), sid(100), "beta", 2, 1},
		{tid(12), "bad", "", "beta", 2, 0},
		{tid(13), sid(103), "", "beta", 2, 0},
		{tid(14), sid(104), "0000000000000000", "beta", 2, 0},
		{tid(15), sid(105), "", "alpha", 3, 0}, {tid(15), sid(106), sid(105), "", 2, 0},
		{tid(16), "0000000000000000", "", "beta", 2, 0},
		{tid(17), sid(107), "", "alpha", 3, 0},
		{tid(17), sid(108), sid(107), "beta", 2, 0}, {tid(17), sid(108), sid(107), "beta", 2, 1},
		{"ABCDEF0123456789ABCDEF0123456789", "ABCDEF0123456789", "", "gamma", 3, -1},
		{"abcdef0123456789abcdef0123456789", sid(102), "abcdef0123456789", "delta", 2, 0},
	}
	var resources []any
	for _, s := range spans {
		ts := fmt.Sprint(base.Add(time.Duration(s.offset) * time.Second).UnixNano())
		item := map[string]any{"traceId": s.trace, "spanId": s.id, "name": "test", "kind": s.kind, "startTimeUnixNano": ts, "endTimeUnixNano": ts}
		if s.parent != "" {
			item["parentSpanId"] = s.parent
		}
		resources = append(resources, map[string]any{"resource": map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": s.service}}}}, "scopeSpans": []any{map[string]any{"spans": []any{item}}}})
	}
	data := filepath.Join(dir, "data", "traces")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"resourceSpans": resources})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "traces.jsonl"), append(line, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := IngestAll(db, filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	report, err := QueryDependencies(db, DependencyOptions{Since: base, Until: base})
	if err != nil {
		t.Fatal(err)
	}
	wantEdges := []DependencyEdge{{"alpha", "beta", 1}, {"gamma", "delta", 1}}
	wantExcluded := DependencyExclusions{InvalidChildID: 2, DuplicateChild: 3, InvalidParentID: 2, MissingParent: 2, DuplicateParent: 1, UnknownService: 2, NonClientParent: 1, SameService: 1}
	if !reflect.DeepEqual(report.Edges, wantEdges) || report.Exclusions != wantExcluded {
		t.Fatalf("report: %+v, exclusions: %+v", report.Edges, report.Exclusions)
	}
	empty, err := QueryDependencies(db, DependencyOptions{Since: base.Add(2 * time.Second), Until: base.Add(2 * time.Second)})
	if err != nil || len(empty.Edges) != 0 || empty.Edges == nil || empty.Exclusions != (DependencyExclusions{}) {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	if _, err := QueryDependencies(db, DependencyOptions{Since: base.Add(time.Second), Until: base}); err == nil {
		t.Fatal("accepted reversed window")
	}
}
