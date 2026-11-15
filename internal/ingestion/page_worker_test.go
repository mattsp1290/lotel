package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/mattsp1290/lotel/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPageMigrationWithRunningIngestionWorker(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := os.MkdirAll(filepath.Join(root, "traces"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "traces", "traces.jsonl")
	appendSpan := func(service string) {
		t.Helper()
		payload := fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":%q}}]},"scopeSpans":[{"spans":[{"traceId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","spanId":"bbbbbbbbbbbbbbbb","name":"probe","startTimeUnixNano":"1700000000000000000","endTimeUnixNano":"1700000001000000000"}]}]}]}`, service)
		if !json.Valid([]byte(payload)) {
			t.Fatal("invalid fixture")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err = f.WriteString(payload + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	appendSpan("before")
	worker := New(Options{Enabled: true, Interval: 150 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx, root, nil) }()
	defer func() { cancel(); <-done }()
	wait := func(label string, pred func() bool) {
		t.Helper()
		end := time.Now().Add(8 * time.Second)
		for time.Now().Before(end) {
			if pred() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s; status=%+v", label, worker.Status())
	}
	wait("first ingestion", func() bool { return worker.Status().LastSuccess != nil })
	dbpath := filepath.Join(root, "lotel.db")
	var before int64
	if err := storage.WithDB(context.Background(), dbpath, func(db *sql.DB) error {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM traces").Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("first count %d", n)
		}
		_, err := storage.QueryPage(db, storage.PageRequest{Signal: "traces", Since: time.Unix(1699999990, 0), Until: time.Unix(1700000010, 0), Limit: 1})
		if err != nil {
			return err
		}
		return db.QueryRow("SELECT cursor_id FROM traces").Scan(&before)
	}); err != nil {
		t.Fatal(err)
	}
	appendSpan("after")
	wait("post-migration ingestion", func() bool {
		found := false
		_ = storage.WithDB(context.Background(), dbpath, func(db *sql.DB) error {
			var id int64
			err := db.QueryRow("SELECT cursor_id FROM traces WHERE service_name='after'").Scan(&id)
			found = err == nil && id > before
			return nil
		})
		return found
	})
	locked := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- storage.WithDB(context.Background(), dbpath, func(_ *sql.DB) error { close(locked); <-release; return nil })
	}()
	<-locked
	wait("lock timeout", func() bool { return strings.Contains(worker.Status().LastError, "waiting for telemetry database") })
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	wait("worker recovery", func() bool { return worker.Status().LastError == "" })
	t.Log("PASS exact v3 ingestion worker resumed after additive migration and recovered from held lock")
}
