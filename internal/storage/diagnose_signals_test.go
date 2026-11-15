package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDiagnoseSignalsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lotel.db")
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	if got := DiagnoseSignals(context.Background(), path, "app", start, end); got.Observation != "unavailable" || got.Reason != "data_missing" {
		t.Fatalf("missing: %+v", got)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("created lock: %v", err)
	}
	err := WithDB(context.Background(), path, func(db *sql.DB) error {
		if _, err := db.Exec(`INSERT INTO traces (trace_id,span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,date) VALUES ('a','b','request',1,?,?,0,0,'app','2025-01-01')`, start, start); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO logs (timestamp,service_name,date) VALUES (?, 'app', '2025-01-01')`, end); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	got := DiagnoseSignals(context.Background(), path, "app", start, end)
	if got.Observation != "available" || len(got.Signals) != 3 || got.Signals[0].ObservedInWindow == nil || !*got.Signals[0].ObservedInWindow || got.Signals[1].ObservedInWindow == nil || *got.Signals[1].ObservedInWindow || got.Signals[2].LatestInWindow == nil || !got.Signals[2].LatestInWindow.Equal(end) {
		t.Fatalf("unexpected: %+v", got)
	}
	lock, err := openExistingDiagnosticFile(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := tryDatabaseLock(lock)
	if err != nil || !acquired {
		t.Fatalf("diagnosis retained the database lock: acquired=%t, error=%v", acquired, err)
	}
	releaseDatabaseLock(lock)
	lock.Close()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != digest {
		t.Fatal("database changed")
	}
	got = DiagnoseSignals(context.Background(), path, "app", start.Add(time.Nanosecond), end.Add(-time.Nanosecond))
	if got.Observation != "available" || *got.Signals[0].ObservedInWindow || *got.Signals[2].ObservedInWindow {
		t.Fatalf("nanosecond window: %+v", got)
	}
	err = WithDB(context.Background(), path, func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE schema_meta SET value='2' WHERE key='schema_version'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got = DiagnoseSignals(context.Background(), path, "app", start, end)
	if got.Observation != "unavailable" || got.Reason != "schema_unavailable" {
		t.Fatalf("schema: %+v", got)
	}
}

func TestDiagnoseSignalsUnsafeAndBusy(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	for _, name := range []string{"bad#path", "bad?path", "bad%path"} {
		path := filepath.Join(t.TempDir(), name, "lotel.db")
		got := DiagnoseSignals(context.Background(), path, "app", start, end)
		if got.Reason != "unsafe_path" {
			t.Fatalf("%q: %+v", name, got)
		}
	}
	path := filepath.Join(t.TempDir(), "lotel.db")
	err := WithDB(context.Background(), path, func(db *sql.DB) error {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		got := DiagnoseSignals(ctx, path, "app", start, end)
		if got.Reason != "data_busy" {
			t.Errorf("busy: %+v", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDiagnoseSignalsWALHelper(t *testing.T) {
	path := os.Getenv("LOTEL_DIAGNOSTIC_WAL_HELPER")
	if path == "" {
		return
	}
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO logs (timestamp,service_name,date) VALUES ('2025-01-01 00:00:00','app','2025-01-01')`); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestDiagnoseSignalsPreservesWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lotel.db")
	if err := WithDB(context.Background(), path, func(db *sql.DB) error { return nil }); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDiagnoseSignalsWALHelper$")
	cmd.Env = append(os.Environ(), "LOTEL_DIAGNOSTIC_WAL_HELPER="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, out)
	}
	wal := path + ".wal"
	beforeDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeWAL, err := os.ReadFile(wal)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got := DiagnoseSignals(context.Background(), path, "app", start, start.Add(time.Second))
	if got.Observation != "available" || got.Signals[2].ObservedInWindow == nil || !*got.Signals[2].ObservedInWindow {
		t.Fatalf("WAL result: %+v", got)
	}
	afterDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterWAL, err := os.ReadFile(wal)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(beforeDB) != sha256.Sum256(afterDB) || sha256.Sum256(beforeWAL) != sha256.Sum256(afterWAL) {
		t.Fatal("diagnosis modified database or WAL")
	}
}

func TestDiagnoseSignalsUnavailableDatabase(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	dir := filepath.Join(t.TempDir(), "path with spaces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "lotel.db")
	if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	got := DiagnoseSignals(context.Background(), path, "app", start, end)
	if got.Reason != "data_missing" {
		t.Fatalf("missing database: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("database created: %v", err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got = DiagnoseSignals(context.Background(), path, "app", start, end)
	if got.Observation != "unavailable" {
		t.Fatalf("corrupt database: %+v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("corrupt database changed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := WithDB(context.Background(), path, func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE schema_meta SET value='4' WHERE key='schema_version'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got = DiagnoseSignals(context.Background(), path, "app", start, end)
	if got.Reason != "schema_unavailable" {
		t.Fatalf("new schema: %+v", got)
	}
}
