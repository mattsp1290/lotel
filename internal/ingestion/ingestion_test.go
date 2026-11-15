package ingestion

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestRunOptionsAndCancellation(t *testing.T) {
	if err := Run(context.Background(), t.TempDir(), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), t.TempDir(), Options{Enabled: true}, nil); err == nil {
		t.Fatal("enabled ingestion accepted a zero interval")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, t.TempDir(), Options{Enabled: true, Interval: time.Hour}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunReleasesDatabaseForOtherProcesses(t *testing.T) {
	if root := os.Getenv("LOTEL_INGESTION_TEST_ROOT"); root != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := Run(ctx, root, Options{Enabled: true, Interval: 50 * time.Millisecond}, func(err error) {
			fmt.Fprintln(os.Stderr, err)
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	dir := filepath.Join(root, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000000","body":{"stringValue":"automatic ingestion"}}]}]}]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "logs.jsonl"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunReleasesDatabaseForOtherProcesses$")
	cmd.Env = append(os.Environ(), "LOTEL_INGESTION_TEST_ROOT="+root)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	deadline := time.Now().Add(2 * time.Second)
	queries := 0
	appended := false
	for time.Now().Before(deadline) {
		db, err := storage.OpenDB(filepath.Join(root, "lotel.db"))
		if err == nil {
			rows, queryErr := storage.QueryLogs(db, storage.LogQueryOptions{})
			closeErr := db.Close()
			if queryErr == nil && closeErr == nil && len(rows) == 1 && !appended {
				file, err := os.OpenFile(filepath.Join(dir, "logs.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteString(payload)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("appending telemetry: %v, %v", err, closeErr)
				}
				appended = true
			}
			if queryErr == nil && closeErr == nil && len(rows) == 2 && rows[0].Body == "automatic ingestion" {
				queries++
				if queries == 3 {
					cancel()
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("queried automatic rows from a separate process %d times, want 3", queries)
}

func TestRunRecoversAfterIngestionError(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "logs")
	if err := os.Mkdir(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000000","body":{"stringValue":"recovered ingestion"}}]}]}]}` + "\n"
	logPath := filepath.Join(logDir, "logs.jsonl")
	if err := os.Mkdir(logPath, 0700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "lotel.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failures := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, root, Options{Enabled: true, Interval: 10 * time.Millisecond}, func(err error) {
			select {
			case failures <- err:
			default:
			}
		})
	}()
	select {
	case <-failures:
	case <-time.After(2 * time.Second):
		t.Fatal("ingestion did not report the unreadable source")
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		db, err := sql.Open("duckdb", dbPath)
		if err == nil {
			var rows int
			queryErr := db.QueryRow("SELECT count(*) FROM logs WHERE body='recovered ingestion'").Scan(&rows)
			db.Close()
			if queryErr == nil && rows == 1 {
				cancel()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ingestion did not recover after the source became readable")
}
