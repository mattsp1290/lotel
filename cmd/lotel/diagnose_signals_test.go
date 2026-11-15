package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestDiagnoseSignalsCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lotel")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	home := t.TempDir()
	invoke := func(args ...string) ([]byte, error) {
		c := exec.Command(binary, args...)
		c.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
		return c.CombinedOutput()
	}
	out, err := invoke("diagnose", "signals", "--service", "app", "--since", "1h")
	if err != nil {
		t.Fatalf("missing data: %v %s", err, out)
	}
	validateOutput(t, "diagnose signals", out)
	var report storage.SignalDiagnosis
	if err = json.Unmarshal(out, &report); err != nil || report.Observation != "unavailable" || report.Reason != "data_missing" {
		t.Fatalf("report: %s %v", out, err)
	}
	for _, service := range []string{strings.Repeat("\x01", 4096), strings.Repeat("a", 4096)} {
		out, err = invoke("diagnose", "signals", "--service", service, "--since", "1h")
		if err != nil {
			t.Fatalf("boundary service: %v %s", err, out)
		}
		if err = json.Unmarshal(out, &report); err != nil || report.Service != service || report.Reason != "data_missing" {
			t.Fatalf("boundary service report: %s %v", out, err)
		}
	}
	if runtime.GOOS != "windows" {
		out, err = invoke("diagnose", "signals", "--service", string([]byte{'x', 0xff}), "--since", "1h")
		if err == nil {
			t.Fatalf("accepted invalid UTF-8: %s", out)
		}
	}
	if out, err = invoke("diagnose", "signals", "--service", strings.Repeat("a", 4097), "--since", "1h"); err == nil {
		t.Fatalf("accepted oversized service: %s", out)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("changed fresh home: %v %v", entries, err)
	}
	for _, args := range [][]string{{"diagnose", "signals", "--since", "1h"}, {"diagnose", "signals", "--service", "app", "--since", "0s"}, {"diagnose", "signals", "--service", "app", "--since", "1h", "--until", ""}} {
		if out, err = invoke(args...); err == nil {
			t.Errorf("accepted %v: %s", args, out)
		}
	}
	entries, err = os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid input changed home: %v %v", entries, err)
	}
	out, err = invoke("commands")
	if err != nil {
		t.Fatalf("commands: %v %s", err, out)
	}
	if strings.Contains(string(out), "diagnose-signals-worker") {
		t.Fatal("worker exposed in catalog")
	}
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := storage.WithDB(context.Background(), path, func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO traces(trace_id,span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,date) VALUES ('trace','span','request',1,'2025-01-02 12:00:00','2025-01-02 12:00:00',0,0,'app','2025-01-02')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	out, err = invoke("diagnose", "signals", "--service", "app", "--since", "2025-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("seeded diagnosis: %v %s", err, out)
	}
	validateOutput(t, "diagnose signals", out)
	if err := json.Unmarshal(out, &report); err != nil || report.Observation != "available" || report.Signals[0].ObservedInWindow == nil || !*report.Signals[0].ObservedInWindow {
		t.Fatalf("seeded report: %s %v", out, err)
	}
}

func TestSignalWorkerTimeoutAndInvalidResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "worker")
	req := signalDiagnosticRequest{Service: "app", Since: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := runSignalWorker(ctx, executable, req)
	if got.Observation != "unavailable" || got.Reason != "worker_timeout" || time.Since(start) > 2*time.Second {
		t.Fatalf("timeout: %+v", got)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '{\"schema_version\":1,\"reason\":\"secret\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got = runSignalWorker(context.Background(), executable, req)
	if got.Observation != "unavailable" || got.Reason != "worker_unavailable" {
		t.Fatalf("invalid response: %+v", got)
	}
	valid, err := json.Marshal(unavailableSignalReport(req, "data_missing"))
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s' '" + string(valid) + "'\ndd if=/dev/zero bs=1024 count=70 2>/dev/null | tr '\\000' ' '\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	got = runSignalWorker(context.Background(), executable, req)
	if got.Observation != "unavailable" || got.Reason != "worker_unavailable" {
		t.Fatalf("over-limit valid response accepted: %+v", got)
	}
}
