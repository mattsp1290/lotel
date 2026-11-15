package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestTraceCommandCorrelatesRecords(t *testing.T) {
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
	const traceID = "abcdef0123456789abcdef0123456789"
	for _, span := range []struct{ trace, id, parent, service string }{
		{traceID, "1111111111111111", "", "frontend"},
		{traceID, "2222222222222222", "1111111111111111", "backend"},
		{traceID, "2222222222222222", "missing", "backend"},
		{"12345678901234567890123456789012", "3333333333333333", "", "other"},
	} {
		_, err := db.Exec(`INSERT INTO traces (trace_id,span_id,parent_span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,attributes,date) VALUES (?,?,?,'request',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,0,0,?,'{}',CURRENT_DATE)`, span.trace, span.id, span.parent, span.service)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, log := range []struct{ trace, span, body string }{
		{traceID, "2222222222222222", "linked"},
		{traceID, "unknown", "unlinked"},
		{"12345678901234567890123456789012", "3333333333333333", "other"},
		{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "unknown", "log without a span"},
	} {
		_, err := db.Exec(`INSERT INTO logs (timestamp,severity_number,body,service_name,trace_id,span_id,date) VALUES (CURRENT_TIMESTAMP,9,?,'backend',?,?,CURRENT_DATE)`, log.body, log.trace, log.span)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := traceCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{strings.ToUpper(traceID)})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result traceBundle
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TraceID != traceID || len(result.Spans) != 3 || len(result.Logs) != 2 {
		t.Fatalf("unexpected bundle: %s", out.String())
	}
	services, parents, bodies := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, span := range result.Spans {
		services[span.ServiceName] = true
		parents[span.ParentSpanID] = true
	}
	for _, log := range result.Logs {
		bodies[log.Body] = true
	}
	if !services["frontend"] || !services["backend"] || !parents["1111111111111111"] || !parents["missing"] || !bodies["linked"] || !bodies["unlinked"] {
		t.Fatalf("missing correlated data: %s", out.String())
	}

	out.Reset()
	cmd = traceCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"spans": []`) || !strings.Contains(out.String(), `"logs": []`) {
		t.Fatalf("empty trace must return arrays: %s", out.String())
	}

	out.Reset()
	cmd = traceCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Spans) != 0 || len(result.Logs) != 1 || result.Logs[0].Body != "log without a span" {
		t.Fatalf("lost log without a span: %s", out.String())
	}
}

func TestTraceCommandRejectsInvalidIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, args := range [][]string{{}, {"0"}, {strings.Repeat("0", 32)}, {strings.Repeat("z", 32)}, {strings.Repeat("a", 32), "extra"}} {
		cmd := traceCommand()
		cmd.SetArgs(args)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected invalid arguments %q to fail", args)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("invalid arguments opened database: %v", err)
	}
}

func TestTraceCommandMatchesIngestedMixedCaseIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataPath := filepath.Join(home, ".lotel", "data")
	files := map[string]string{
		"traces": `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"mixed-case"}}]},"scopeSpans":[{"spans":[{"traceId":"ABCDEF0123456789ABCDEF0123456789","spanId":"1111111111111111","name":"request","startTimeUnixNano":"1700000000000000000","endTimeUnixNano":"1700000001000000000"}]}]}]}` + "\n",
		"logs":   `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"mixed-case"}}]},"scopeLogs":[{"logRecords":[{"traceId":"abcdef0123456789abcdef0123456789","spanId":"1111111111111111","timeUnixNano":"1700000000000000000","body":{"stringValue":"linked"}},{"traceId":"ABCDEF0123456789ABCDEF0123456789","spanId":"1111111111111111","timeUnixNano":"1700000000000000001","body":{"stringValue":"uppercase"}}]}]}]}` + "\n",
	}
	for signal, data := range files {
		dir := filepath.Join(dataPath, signal)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, signal+".jsonl"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.OpenDB(filepath.Join(dataPath, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.IngestAll(db, dataPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := traceCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"aBcDeF0123456789ABCDEF0123456789"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result traceBundle
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Spans) != 1 || len(result.Logs) != 2 {
		t.Fatalf("missing ingested span or log: %s", out.String())
	}
}
