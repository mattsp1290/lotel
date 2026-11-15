package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestLogFiltersCLI(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(data, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, row := range []struct {
		severity      int
		body, service string
	}{
		{0, "text only severity", "app"}, {16, "warning", "app"}, {17, "Error 100%_\\'", "app"},
		{20, `{"message":"Structured Error"}`, "app"}, {21, "fatal", "app"}, {24, "last", "app"},
		{25, "invalid", "app"}, {17, "", "app"}, {17, "error other", "other"},
	} {
		traceID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		if i == 2 {
			traceID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}
		_, err := db.Exec(`INSERT INTO logs (timestamp,severity_number,body,service_name,trace_id,date) VALUES (?,?,?,?,?,?)`, start.Add(time.Duration(i)*time.Second), row.severity, row.body, row.service, traceID, start.Format("2006-01-02"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(home string, args ...string) ([]storage.LogResult, string, error) {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--", "query", "logs"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, string(output), err
		}
		var logs []storage.LogResult
		if err := json.Unmarshal(output, &logs); err != nil {
			t.Fatalf("invalid JSON %q: %v", output, err)
		}
		return logs, string(output), nil
	}
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"severity", []string{"--service", "app", "--min-severity", "error"}, []string{"Error 100%_\\'", `{"message":"Structured Error"}`, "fatal", "last", ""}},
		{"fatal", []string{"--service", "app", "--min-severity", "fatal"}, []string{"fatal", "last"}},
		{"literal punctuation", []string{"--body-contains", "%_\\'"}, []string{"Error 100%_\\'"}},
		{"case and structured", []string{"--service", "app", "--min-severity", "error", "--body-contains", "ERROR"}, []string{"Error 100%_\\'", `{"message":"Structured Error"}`}},
		{"trace combined", []string{"--service", "app", "--trace-id", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--min-severity", "error", "--body-contains", "error"}, []string{"Error 100%_\\'"}},
		{"filter before limit", []string{"--service", "app", "--body-contains", "error", "--since", "2026-01-01T00:00:03Z", "--limit", "1"}, []string{`{"message":"Structured Error"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs, output, err := invoke(home, tc.args...)
			if err != nil {
				t.Fatalf("query failed: %v: %s", err, output)
			}
			var got []string
			for _, log := range logs {
				got = append(got, log.Body)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") || len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, args := range [][]string{{"--min-severity", "bogus"}, {"--min-severity="}, {"--body-contains="}} {
		emptyHome := t.TempDir()
		if _, _, err := invoke(emptyHome, args...); err == nil {
			t.Fatalf("accepted invalid flags %q", args)
		}
		if _, err := os.Stat(filepath.Join(emptyHome, ".lotel")); !os.IsNotExist(err) {
			t.Fatalf("invalid flags opened database: %v", err)
		}
	}
}

func TestLogBodyFilterMatchesIngestedStructuredBody(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"structured"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1700000000000000000","severityNumber":17,"body":{"kvlistValue":{"values":[{"key":"message","value":{"stringValue":"Structured ERROR"}}]}}}]}]}]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "logs.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(data, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := storage.IngestAll(db, data); err != nil {
		t.Fatal(err)
	}
	needle := "structured error"
	logs, err := storage.QueryLogs(db, storage.LogQueryOptions{MinSeverity: 17, BodyContains: &needle})
	if err != nil || len(logs) != 1 || !strings.Contains(logs[0].Body, "Structured ERROR") {
		t.Fatalf("structured body not found: %#v, %v", logs, err)
	}
}
