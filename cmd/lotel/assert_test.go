package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestAssertSignalCLI(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(data, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		query   string
		when    time.Time
		service string
	}{
		{`INSERT INTO traces (trace_id,span_id,name,start_time,service_name,date) VALUES ('t','s','test',?,?,CURRENT_DATE)`, at, "app"},
		{`INSERT INTO metrics (metric_name,metric_type,timestamp,service_name,date) VALUES ('m','gauge',?,?,CURRENT_DATE)`, at, "app"},
		{`INSERT INTO logs (timestamp,service_name,date) VALUES (?,?,CURRENT_DATE)`, at, "app"},
		{`INSERT INTO logs (timestamp,service_name,date) VALUES (?,?,CURRENT_DATE)`, at.Add(time.Hour), "app"},
		{`INSERT INTO logs (timestamp,service_name,date) VALUES (?,?,CURRENT_DATE)`, at.Add(time.Minute), "other"},
	} {
		if _, err := db.Exec(entry.query, entry.when, entry.service); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(dir string, args ...string) (string, string, error) {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--", "assert", "signal"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+dir, "LOTEL_CLI_HELPER=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	start := at.Format(time.RFC3339)
	for _, signal := range []string{"traces", "metrics", "logs"} {
		t.Run(signal, func(t *testing.T) {
			out, stderr, err := invoke(home, "--signal", signal, "--service", "app", "--since", start, "--until", start)
			if err != nil {
				t.Fatalf("assert failed: %v: %s", err, stderr)
			}
			var result signalAssertionResult
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Passed || result.LatestTimestamp == nil || !result.LatestTimestamp.Equal(at) {
				t.Fatalf("unexpected result: %s", out)
			}
		})
	}
	for _, timestamp := range []string{"2025-01-02T13:30:00.000000000+01:30", "2025-01-02T06:30:00-05:30"} {
		out, stderr, err := invoke(home, "--signal", "logs", "--service", "app", "--since", timestamp, "--until", timestamp)
		if err != nil || !strings.Contains(out, `"passed":true`) {
			t.Fatalf("valid offset or precision rejected: %q %q %q %v", timestamp, out, stderr, err)
		}
	}
	out, stderr, err := invoke(home, "--signal", "logs", "--service", "app", "--since", "2025-01-02T11:59:59.999999999Z", "--until", "2025-01-02T12:00:00.000000001Z")
	if err != nil || !strings.Contains(out, `"passed":true`) {
		t.Fatalf("valid nanosecond window rejected: %q %q %v", out, stderr, err)
	}
	for _, window := range []struct{ since, until time.Time }{
		{at.Add(time.Nanosecond), at.Add(time.Second)},
		{at.Add(-time.Second), at.Add(-time.Nanosecond)},
		{at.Add(time.Nanosecond), at.Add(999 * time.Nanosecond)},
	} {
		out, stderr, err := invoke(home, "--signal", "traces", "--service", "app", "--since", window.since.Format(time.RFC3339Nano), "--until", window.until.Format(time.RFC3339Nano))
		if err == nil || stderr != "" {
			t.Fatalf("sub-microsecond window should fail: %q %q %v", out, stderr, err)
		}
		var result signalAssertionResult
		if err := json.Unmarshal([]byte(out), &result); err != nil || result.Passed || !result.Since.Equal(window.since) || !result.Until.Equal(window.until) {
			t.Fatalf("wrong window result: %q %v", out, err)
		}
	}
	out, stderr, err = invoke(home, "--signal", "logs", "--service", "app", "--since", at.Add(time.Second).Format(time.RFC3339), "--until", at.Add(30*time.Minute).Format(time.RFC3339))
	if err == nil || stderr != "" {
		t.Fatalf("no match should fail without stderr: %v %q", err, stderr)
	}
	var absent signalAssertionResult
	if err := json.Unmarshal([]byte(out), &absent); err != nil || absent.Passed || absent.LatestTimestamp != nil {
		t.Fatalf("unexpected absence: %q %v", out, err)
	}
	out, stderr, err = invoke(home, "--signal", "traces", "--service", "app", "--since", "99999d")
	if err != nil || !strings.Contains(out, `"passed":true`) {
		t.Fatalf("relative duration: %q %q %v", out, stderr, err)
	}

	for _, args := range [][]string{
		{}, {"--signal", "profiles", "--service", "app", "--since", "1h"},
		{"--signal", "logs", "--service", " ", "--since", "1h"},
		{"--signal", "logs", "--service", "app", "--since", "0s"},
		{"--signal", "logs", "--service", "app", "--since", "0001-01-01T00:00:00Z"},
		{"--signal", "logs", "--service", "app", "--since", "-1h"},
		{"--signal", "logs", "--service", "app", "--since", "1day"},
		{"--signal", "logs", "--service", "app", "--since", "999999999999999999999d"},
		{"--signal", "logs", "--service", "app", "--since", "2025-01-02T12:00:00.0000000001Z", "--until", "2025-01-02T12:00:00Z"},
		{"--signal", "logs", "--service", "app", "--since", "2025-01-03T12:00:00+24:00"},
		{"--signal", "logs", "--service", "app", "--since", "2025-01-02T13:00:00+00:60"},
		{"--signal", "logs", "--service", "app", "--since", "1h", "--until", ""},
		{"--signal", "logs", "--service", "app", "--since", "1h", "--until", "2025-01-02T12:00:00.0000000001Z"},
		{"--signal", "logs", "--service", "app", "--since", "1h", "--until", "2025-01-03T12:00:00+24:00"},
		{"--signal", "logs", "--service", "app", "--since", "1h", "--until", "2025-01-02T13:00:00+00:60"},
		{"--signal", "logs", "--service", "app", "--since", "2025-01-03T00:00:00Z", "--until", "2025-01-02T00:00:00Z"},
		{"--signal", "logs", "--service", "app", "--since", "1h", "--until", "9999-01-01T00:00:00Z"},
	} {
		unused := t.TempDir()
		_, stderr, err := invoke(unused, args...)
		if err == nil || stderr == "" {
			t.Fatalf("invalid args accepted: %v", args)
		}
		if _, err := os.Stat(filepath.Join(unused, ".lotel")); !os.IsNotExist(err) {
			t.Fatalf("invalid args opened DB: %v", args)
		}
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".lotel"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err = invoke(broken, "--signal", "logs", "--service", "app", "--since", "1h")
	if err == nil || stderr == "" || out != "" {
		t.Fatalf("database error misreported: %q %q %v", out, stderr, err)
	}
}
