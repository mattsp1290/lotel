package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

type blockedOutput struct {
	entered chan struct{}
	release chan struct{}
}

type failingOutput struct{ err error }

func (out failingOutput) Write([]byte) (int, error) { return 0, out.err }

func (out *blockedOutput) Write(p []byte) (int, error) {
	close(out.entered)
	<-out.release
	return len(p), nil
}

func TestRecentReleasesDatabaseBeforeOutputDrains(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	for _, signal := range []string{"traces", "logs"} {
		t.Run(signal, func(t *testing.T) {
			out := &blockedOutput{entered: make(chan struct{}), release: make(chan struct{})}
			released := false
			defer func() {
				if !released {
					close(out.release)
				}
			}()
			cmd := recentCommand()
			cmd.SetOut(out)
			cmd.SetArgs([]string{signal, "--since", "2025-01-01T00:00:00Z", "--until", "2025-01-01T00:00:01Z", "--limit", "1"})
			done := make(chan error, 1)
			go func() { done <- cmd.Execute() }()
			select {
			case <-out.entered:
			case err := <-done:
				t.Fatalf("command finished before writing: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("command did not start writing")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := storage.WithDB(ctx, path, func(db *sql.DB) error { return db.PingContext(ctx) }); err != nil {
				t.Fatalf("output blocked database access: %v", err)
			}
			close(out.release)
			released = true
			if err := <-done; err != nil {
				t.Fatalf("query failed after output resumed: %v", err)
			}
		})
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestRecentPropagatesOutputErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := errors.New("output failed")
	cmd := recentCommand()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetOut(failingOutput{err: want})
	cmd.SetArgs([]string{"traces", "--since", "2025-01-01T00:00:00Z", "--until", "2025-01-01T00:00:01Z", "--limit", "1"})
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Fatalf("output error lost: %v", err)
	}
}
