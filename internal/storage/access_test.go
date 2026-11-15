package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWithDBSerializesProcessesAndReleasesAfterError(t *testing.T) {
	if path := os.Getenv("LOTEL_ACCESS_TEST_DB"); path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := os.WriteFile(os.Getenv("LOTEL_ACCESS_TEST_READY"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := WithDB(ctx, path, func(db *sql.DB) error { return os.WriteFile(os.Getenv("LOTEL_ACCESS_TEST_ACQUIRED"), nil, 0o600) }); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	path := filepath.Join(root, "lotel.db")
	entered := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	sentinel := errors.New("callback failure")
	go func() {
		holderDone <- WithDB(context.Background(), path, func(db *sql.DB) error { close(entered); <-release; return sentinel })
	}()
	<-entered
	ready, acquired := filepath.Join(root, "ready"), filepath.Join(root, "acquired")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWithDBSerializesProcessesAndReleasesAfterError$")
	child.Env = append(os.Environ(), "LOTEL_ACCESS_TEST_DB="+path, "LOTEL_ACCESS_TEST_READY="+ready, "LOTEL_ACCESS_TEST_ACQUIRED="+acquired)
	if err := child.Start(); err != nil {
		close(release)
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("child did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	if _, err := os.Stat(acquired); !os.IsNotExist(err) {
		close(release)
		t.Fatalf("second process entered while first owned database: %v", err)
	}
	blockedCtx, blockedCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := WithDB(blockedCtx, path, func(db *sql.DB) error { t.Error("canceled waiter entered database"); return nil })
	blockedCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("lock wait did not honor context: %v", err)
	}
	close(release)
	if err := <-holderDone; !errors.Is(err, sentinel) {
		t.Fatalf("callback error lost: %v", err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("second process failed after release: %v", err)
	}
	if _, err := os.Stat(acquired); err != nil {
		t.Fatal("second process never acquired database")
	}
	if err := WithDB(ctx, path, func(db *sql.DB) error { return db.PingContext(ctx) }); err != nil {
		t.Fatalf("database stayed locked after callback error: %v", err)
	}
}

func TestDuckDBCancellationReleasesDatabaseLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	outer, cancelOuter := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOuter()
	err := WithDB(outer, path, func(db *sql.DB) error {
		ctx, cancel := context.WithTimeout(outer, 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		var x float64
		err := db.QueryRowContext(ctx, `SELECT sum(sin(i)) FROM range(1000000000000) t(i)`).Scan(&x)
		if err == nil || ctx.Err() == nil {
			t.Errorf("heavy query did not cancel: %v", err)
		}
		if time.Since(started) > 3*time.Second {
			t.Errorf("query did not promptly stop: %v", time.Since(started))
		}
		return err
	})
	if err == nil {
		t.Fatal("expected interrupted query")
	}
	acquire, cancelAcquire := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelAcquire()
	if err := WithDB(acquire, path, func(db *sql.DB) error { return db.PingContext(acquire) }); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}
