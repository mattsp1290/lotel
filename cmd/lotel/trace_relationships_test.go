package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestTraceRelationshipsPreserveAmbiguousRecords(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	span := func(name, id, parent, service string) storage.TraceResult {
		return storage.TraceResult{Name: name, SpanID: id, ParentSpanID: parent, ServiceName: service, StartTime: now, EndTime: now}
	}
	bundle := traceBundle{TraceID: strings.Repeat("a", 32), Spans: []storage.TraceResult{
		span("child", "2222222222222222", "1111111111111111", "backend"),
		span("root", "1111111111111111", "", "frontend"),
		span("other root", "3333333333333333", "", "other"),
		span("duplicate", "aBcDeF0123456789", "", "other"),
		span("duplicate", "ABCDEF0123456789", "", "other"),
		span("identical", "dddddddddddddddd", "", "other"),
		span("identical", "dddddddddddddddd", "", "other"),
		span("ambiguous", "4444444444444444", "abcdef0123456789", "other"),
		span("missing", "5555555555555555", "9999999999999999", "other"),
		span("invalid", "6666666666666666", "0000000000000000", "other"),
		span("self", "7777777777777777", "7777777777777777", "other"),
		span("cycle a", "8888888888888888", "aaaaaaaaaaaaaaaa", "other"),
		span("cycle b", "aaaaaaaaaaaaaaaa", "8888888888888888", "other"),
		span("enters cycle", "bbbbbbbbbbbbbbbb", "8888888888888888", "other"),
	}, Logs: []storage.LogResult{
		{Timestamp: now, Body: "linked", SpanID: "2222222222222222"},
		{Timestamp: now, Body: "unscoped"},
		{Timestamp: now, Body: "ambiguous", SpanID: "AbCdEf0123456789"},
		{Timestamp: now, Body: "missing", SpanID: "9999999999999999"},
		{Timestamp: now, Body: "invalid", SpanID: "not-hex"},
		{Timestamp: now, Body: "zero", SpanID: "0000000000000000"},
	}}
	if err := bundle.resolveRelationships(); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Spans) != 14 || len(bundle.Logs) != 6 {
		t.Fatalf("lost records: %+v", bundle)
	}
	spanIndex := map[string]int{}
	for i, s := range bundle.Spans {
		spanIndex[s.Name] = i
	}
	ref := func(name string) traceReference { return bundle.Relationships.SpanParents[spanIndex[name]] }
	if got := ref("root").State; got != "root" {
		t.Fatalf("root: %s", got)
	}
	if got := ref("child"); got.State != "linked" || len(got.SpanIndices) != 1 || got.SpanIndices[0] != spanIndex["root"] {
		t.Fatalf("child: %+v", got)
	}
	for name, state := range map[string]string{"other root": "root", "ambiguous": "ambiguous", "missing": "missing", "invalid": "invalid"} {
		got := ref(name)
		if got.State != state {
			t.Errorf("%s: %+v", name, got)
		}
		if state == "ambiguous" && len(got.SpanIndices) != 2 {
			t.Errorf("ambiguous candidates: %+v", got)
		}
	}
	for i, log := range bundle.Logs {
		want := map[string]string{"linked": "linked", "unscoped": "unscoped", "ambiguous": "ambiguous", "missing": "missing", "invalid": "invalid", "zero": "invalid"}[log.Body]
		got := bundle.Relationships.LogSpans[i]
		if got.State != want {
			t.Errorf("log %s: %+v", log.Body, got)
		}
		if log.Body == "linked" && (len(got.SpanIndices) != 1 || got.SpanIndices[0] != spanIndex["child"]) {
			t.Errorf("linked log: %+v", got)
		}
	}
	cycles := map[string]bool{}
	for _, i := range bundle.Relationships.CycleSpanIndices {
		cycles[bundle.Spans[i].Name] = true
	}
	if !reflect.DeepEqual(cycles, map[string]bool{"self": true, "cycle a": true, "cycle b": true}) {
		t.Fatalf("cycles: %v", cycles)
	}
	first, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.resolveRelationships(); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("ordering changed: %s / %s", first, second)
	}
}

func TestTraceCommandRelationships(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataPath := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(dataPath, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	for _, row := range []struct{ name, span, parent, service string }{
		{"root", "1111111111111111", "", "frontend"}, {"child", "2222222222222222", "1111111111111111", "backend"},
	} {
		_, err := db.Exec(`INSERT INTO traces (trace_id,span_id,parent_span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,attributes,date) VALUES (?,?,?,?,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,0,0,?,'{}',CURRENT_DATE)`, id, row.span, row.parent, row.name, row.service)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`INSERT INTO logs (timestamp,severity_number,body,service_name,trace_id,span_id,date) VALUES (CURRENT_TIMESTAMP,9,'linked','backend',?,'2222222222222222',CURRENT_DATE)`, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) []byte {
		var out bytes.Buffer
		cmd := traceCommand()
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	plain := invoke(id)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(plain, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["relationships"]; exists {
		t.Fatalf("default output changed: %s", plain)
	}
	with := invoke(id, "--relationships")
	var bundle traceBundle
	if err := json.Unmarshal(with, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Spans) != 2 || len(bundle.Logs) != 1 || bundle.Relationships == nil {
		t.Fatalf("unexpected result: %s", with)
	}
	child, root := -1, -1
	for i, s := range bundle.Spans {
		if s.Name == "child" {
			child = i
		}
		if s.Name == "root" {
			root = i
		}
	}
	if child < 0 || root < 0 || !reflect.DeepEqual(bundle.Relationships.SpanParents[child], traceReference{State: "linked", SpanIndices: []int{root}}) || !reflect.DeepEqual(bundle.Relationships.LogSpans[0], traceReference{State: "linked", SpanIndices: []int{child}}) {
		t.Fatalf("incorrect relations: %s", with)
	}
	if again := invoke(id, "--relationships"); !bytes.Equal(with, again) {
		t.Fatalf("unstable output: %s / %s", with, again)
	}
}
