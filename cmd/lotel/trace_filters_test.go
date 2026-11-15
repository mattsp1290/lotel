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

func TestTraceFiltersHelper(t *testing.T) {
	if os.Getenv("LOTEL_FILTERS_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"lotel"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing command arguments")
}

func TestTraceFiltersCLI(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(data, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, row := range []struct {
		name, service string
		status        int
		duration      int64
	}{
		{"negative", "app", 2, -1},
		{"zero", "app", 2, 0},
		{"short", "app", 2, 999999},
		{"boundary", "app", 2, 1000000},
		{"long-ok", "app", 1, 2000000},
		{"long-unset", "app", 0, 3000000},
		{"other-service", "other", 2, 4000000},
		{"long-error", "app", 2, 5000000},
	} {
		start := now.Add(time.Duration(i) * time.Second)
		_, err := db.Exec(`INSERT INTO traces (trace_id, span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, '{}', ?)`, row.name, row.name, row.name, start, start, row.duration, row.status, row.service, start.Format("2006-01-02"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(home string, args ...string) ([]storage.TraceResult, string, error) {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestTraceFiltersHelper$", "--", "query", "traces"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_FILTERS_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, string(output), err
		}
		var spans []storage.TraceResult
		if err := json.Unmarshal(output, &spans); err != nil {
			t.Fatalf("invalid JSON %q: %v", output, err)
		}
		return spans, string(output), nil
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"omitted", []string{"--service", "app"}, "negative,zero,short,boundary,long-ok,long-unset,long-error"},
		{"zero", []string{"--service", "app", "--min-duration", "0"}, "zero,short,boundary,long-ok,long-unset,long-error"},
		{"errors", []string{"--service", "app", "--errors-only"}, "negative,zero,short,boundary,long-error"},
		{"inclusive and limit", []string{"--service", "app", "--errors-only", "--min-duration", "1ms", "--limit", "1"}, "boundary"},
		{"combined window", []string{"--service", "app", "--errors-only", "--min-duration", "1ms", "--since", "2026-01-01T00:00:04Z"}, "long-error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, output, err := invoke(home, tc.args...)
			if err != nil {
				t.Fatalf("query failed: %v: %s", err, output)
			}
			var names []string
			for _, row := range rows {
				names = append(names, row.Name)
			}
			if got := strings.Join(names, ","); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, value := range []string{"-1ns", "nonsense", "100000000000000000000h"} {
		t.Run("invalid "+value, func(t *testing.T) {
			unusedHome := t.TempDir()
			_, _, err := invoke(unusedHome, "--min-duration="+value)
			if err == nil {
				t.Fatal("invalid duration accepted")
			}
			if _, err := os.Stat(filepath.Join(unusedHome, ".lotel")); !os.IsNotExist(err) {
				t.Fatalf("invalid duration opened the database: %v", err)
			}
		})
	}
}
