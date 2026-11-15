package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func checkOutputUnlock(t *testing.T, cmd *cobra.Command, args []string, expected error, stderr bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	out := &blockedOutput{entered: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(out.release)
		}
	}()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if stderr {
		cmd.SetErr(out)
		cmd.SetOut(io.Discard)
	} else {
		cmd.SetOut(out)
	}
	cmd.SetArgs(args)
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-out.entered:
	case err := <-done:
		t.Fatalf("command finished before output: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not start output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := storage.WithDB(ctx, path, func(db *sql.DB) error { return db.PingContext(ctx) }); err != nil {
		t.Fatalf("blocked output retained database: %v", err)
	}
	close(out.release)
	released = true
	if err := <-done; !errors.Is(err, expected) {
		t.Fatalf("unexpected command result: %v", err)
	}
}

func TestTraceReleasesDatabaseBeforeOutput(t *testing.T) {
	checkOutputUnlock(t, traceCommand(), []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--relationships"}, nil, false)
}

func TestQueriesReleaseDatabaseBeforeOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cmd    *cobra.Command
		args   []string
		err    error
		stderr bool
	}{
		{"otlp", otlpQueryCommand(""), []string{"--signal", "traces"}, nil, false},
		{"services", discoveryCommand("services"), nil, nil, false},
		{"operations", discoveryCommand("operations"), nil, nil, false},
		{"span stats", spanStatsCommand(), []string{"--since", "1h"}, nil, false},
		{"log stats", logStatsCommand(), []string{"--since", "1h"}, nil, false},
		{"genai calls", genAICommand(), []string{"calls"}, nil, false},
		{"genai usage", genAICommand(), []string{"usage"}, nil, false},
		{"missing assertion", assertCommand(), []string{"signal", "--signal", "logs", "--service", "missing", "--since", "1h"}, errAssertionFailed, false},
		{"ingest", ingestCommand(), nil, nil, false},
		{"prune", pruneCommand(), []string{"--all"}, nil, false},
		{"prune notice", pruneCommand(), []string{"--all", "--dry-run"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) { checkOutputUnlock(t, tc.cmd, tc.args, tc.err, tc.stderr) })
	}
}

func TestDatabaseJSONPropagatesOutputFailureAfterMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := errors.New("output failed")
	cmd := &cobra.Command{Use: "mutation", SilenceErrors: true, SilenceUsage: true}
	cmd.SetOut(failingOutput{err: want})
	cmd.RunE = withDatabaseJSON(func(_ *cobra.Command, _ []string, db *sql.DB) (any, error) {
		_, err := db.Exec("CREATE TABLE output_lock_mutation (value INTEGER)")
		return map[string]bool{"done": true}, err
	}, true)
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Fatalf("output error: %v", err)
	}
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := storage.WithDB(context.Background(), path, func(db *sql.DB) error {
		var count int
		return db.QueryRow("SELECT count(*) FROM output_lock_mutation").Scan(&count)
	}); err != nil {
		t.Fatalf("mutation did not persist: %v", err)
	}
}

func TestDatabaseJSONDoesNotWriteOnQueryFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := errors.New("query failed")
	var output bytes.Buffer
	cmd := &cobra.Command{Use: "query", SilenceErrors: true, SilenceUsage: true}
	cmd.SetOut(&output)
	cmd.RunE = withDatabaseJSON(func(_ *cobra.Command, _ []string, _ *sql.DB) (any, error) {
		return map[string]bool{"done": true}, want
	}, true)
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Fatalf("query error: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("query failure wrote output: %q", output.String())
	}
}

func TestIngestAndPruneCommitBeforeOutputFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".lotel", "data", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"lock-test"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1700000000000000000","body":{"stringValue":"test"}}]}]}]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "logs.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("output failed")
	countLogs := func() int {
		t.Helper()
		var count int
		err := storage.WithDB(context.Background(), filepath.Join(home, ".lotel", "data", "lotel.db"), func(db *sql.DB) error {
			return db.QueryRow("SELECT count(*) FROM logs").Scan(&count)
		})
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	for _, tc := range []struct {
		cmd   *cobra.Command
		args  []string
		count int
	}{
		{ingestCommand(), nil, 1},
		{pruneCommand(), []string{"--all"}, 0},
	} {
		tc.cmd.SilenceErrors, tc.cmd.SilenceUsage = true, true
		tc.cmd.SetOut(failingOutput{err: want})
		tc.cmd.SetArgs(tc.args)
		if err := tc.cmd.Execute(); !errors.Is(err, want) {
			t.Fatalf("output error: %v", err)
		}
		if got := countLogs(); got != tc.count {
			t.Fatalf("stored logs = %d, want %d", got, tc.count)
		}
	}
}

func TestRebuildWarningReleasesDatabaseOnQueryError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE traces (old INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	want := errors.New("query failed")
	out := &blockedOutput{entered: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(out.release)
		}
	}()
	cmd := &cobra.Command{Use: "query", SilenceErrors: true, SilenceUsage: true}
	cmd.SetErr(out)
	cmd.RunE = withDatabase(func(_ *cobra.Command, _ []string, _ *sql.DB) error { return want })
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-out.entered:
	case err := <-done:
		t.Fatalf("command finished before warning: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("warning did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = storage.WithDB(ctx, path, func(db *sql.DB) error { return db.PingContext(ctx) })
	close(out.release)
	released = true
	if err != nil {
		t.Fatalf("blocked warning retained database: %v", err)
	}
	if err := <-done; !errors.Is(err, want) {
		t.Fatalf("query error: %v", err)
	}
}
