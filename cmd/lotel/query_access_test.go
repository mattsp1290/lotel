package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestQueryCommandsWaitForIngestionAndReleaseDatabase(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const traceID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const at = "2025-01-02T12:00:00Z"
	commands := [][]string{
		{"query", "services"},
		{"query", "operations"},
		{"query", "span-stats", "--since", at, "--until", at},
		{"query", "log-stats", "--since", at, "--until", at},
		{"query", "trace", traceID, "--relationships"},
		{"query", "traces", "--errors-only"},
		{"query", "logs", "--min-severity", "error"},
		{"query", "recent", "traces", "--since", at, "--until", at, "--limit", "1"},
		{"query", "recent", "logs", "--since", at, "--until", at, "--limit", "1"},
		{"assert", "signal", "--signal", "logs", "--service", "locked-query", "--since", at, "--until", at},
	}
	type result struct {
		command []string
		output  []byte
		err     error
	}
	results := make(chan result, len(commands))
	err := storage.WithDB(ctx, path, func(db *sql.DB) error {
		for _, args := range commands {
			cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--"}, args...)...)
			cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
			go func() {
				output, err := cmd.CombinedOutput()
				results <- result{command: args, output: output, err: err}
			}()
		}
		select {
		case result := <-results:
			return fmt.Errorf("%v finished while ingestion owned the database: %v: %s", result.command, result.err, result.output)
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		if _, err := db.Exec(`INSERT INTO traces (trace_id, span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, date) VALUES (?, 'bbbbbbbbbbbbbbbb', 'request', 1, ?, ?, 123, 2, 'locked-query', '2025-01-02')`, traceID, at, at); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO logs (timestamp, severity_number, body, trace_id, span_id, service_name, date) VALUES (?, 17, 'ready', ?, 'bbbbbbbbbbbbbbbb', 'locked-query', '2025-01-02')`, at, traceID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for range commands {
		result := <-results
		if result.err != nil || !json.Valid(result.output) || !strings.Contains(string(result.output), "locked-query") {
			t.Errorf("%v did not observe ingested data: %v: %s", result.command, result.err, result.output)
		}
	}

	t.Setenv("HOME", home)
	assertion := assertCommand()
	assertion.SilenceErrors, assertion.SilenceUsage = true, true
	assertion.SetOut(io.Discard)
	assertion.SetArgs([]string{"signal", "--signal", "logs", "--service", "absent", "--since", at, "--until", at})
	if err := assertion.ExecuteContext(ctx); !errors.Is(err, errAssertionFailed) {
		t.Fatalf("missing evidence result: %v", err)
	}
	if err := storage.WithDB(ctx, path, func(db *sql.DB) error { return db.PingContext(ctx) }); err != nil {
		t.Fatalf("query or failed assertion retained database ownership: %v", err)
	}
}
