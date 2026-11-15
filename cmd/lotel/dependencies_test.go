package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestDependenciesCLI(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(data, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	_, err = db.Exec(`INSERT INTO traces (trace_id, span_id, parent_span_id, name, kind, start_time, end_time, service_name, date) VALUES
		('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '1111111111111111', NULL, 'client', 3, ?, ?, 'alpha', CURRENT_DATE),
		('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '2222222222222222', '1111111111111111', 'server', 2, ?, ?, 'beta', CURRENT_DATE)`, at.Add(-time.Second), at, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(home string, args ...string) (string, string, error) {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--", "query", "dependencies"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		return out.String(), stderr.String(), err
	}
	out, stderr, err := invoke(home, "--since", at.Format(time.RFC3339), "--until", at.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("query: %s %v", stderr, err)
	}
	var report storage.DependencyReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Edges) != 1 || report.Edges[0] != (storage.DependencyEdge{SourceService: "alpha", TargetService: "beta", ObservedCalls: 1}) {
		t.Fatalf("report: %s", out)
	}
	out, stderr, err = invoke(home, "--since", at.Add(time.Nanosecond).Format(time.RFC3339Nano), "--until", at.Add(time.Second).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("fractional query: %s %v", stderr, err)
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil || len(report.Edges) != 0 {
		t.Fatalf("fractional report: %s %v", out, err)
	}
	for _, args := range [][]string{{}, {"--since", "0s"}, {"--since", "1h", "--until", ""}, {"--since", "2025-01-03T00:00:00Z", "--until", "2025-01-02T00:00:00Z"}} {
		unused := t.TempDir()
		_, _, err := invoke(unused, args...)
		if err == nil {
			t.Errorf("accepted %q", args)
		}
		if _, err := os.Stat(filepath.Join(unused, ".lotel")); !os.IsNotExist(err) {
			t.Errorf("opened database for %q: %v", args, err)
		}
	}
}
