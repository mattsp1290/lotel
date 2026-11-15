package collector_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestNativeShutdownDBHolder(t *testing.T) {
	path := os.Getenv("LOTEL_SHUTDOWN_TEST_DB")
	if path == "" {
		t.Skip("subprocess database holder")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		db, err := storage.OpenDB(path)
		if err == nil {
			defer db.Close()
			fmt.Println("ready")
			_, _ = io.Copy(io.Discard, os.Stdin)
			return
		}
		if !storage.IsDatabaseLocked(err) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func holdNativeDatabase(t *testing.T, path string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeShutdownDBHolder$")
	cmd.Env = append(os.Environ(), "LOTEL_SHUTDOWN_TEST_DB="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			stdin.Close()
			if err := cmd.Wait(); err != nil {
				t.Errorf("database holder: %v\n%s", err, stderr.String())
			}
		})
	}
	t.Cleanup(release)
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		release()
		t.Fatalf("database holder did not acquire lock: %v\n%s", scanner.Err(), stderr.String())
	}
	return release
}

func TestNativeShutdownDatabaseContention(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprintf("persistent=%t", persistent), func(t *testing.T) {
			f := newNativeCLIFixture(t, "")
			if out, err := f.run("start", "--backend", "native", "--wait"); err != nil {
				t.Fatalf("start: %v\n%s", err, out)
			}
			release := holdNativeDatabase(t, filepath.Join(f.home, ".lotel", "data", "lotel.db"))
			service := "shutdown-contention-test"
			payload := fmt.Sprintf(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":%q}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":%q,"body":{"stringValue":"flushed during shutdown"}}]}]}]}`, service, fmt.Sprint(time.Now().UnixNano()))
			client := &http.Client{Timeout: 5 * time.Second}
			response, err := client.Post(f.endpoint+"/v1/logs", "application/json", strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("OTLP logs: %s", response.Status)
			}
			type stopResult struct {
				out []byte
				err error
			}
			done := make(chan stopResult, 1)
			go func() {
				out, err := f.run("stop")
				done <- stopResult{out, err}
			}()
			if !persistent {
				select {
				case result := <-done:
					t.Fatalf("stop completed before lock release: %v\n%s", result.err, result.out)
				case <-time.After(200 * time.Millisecond):
				}
				release()
			}
			result := <-done
			if persistent {
				if result.err == nil || !bytes.Contains(result.out, []byte("final ingestion")) {
					t.Fatalf("stop hid final ingestion failure: %v\n%s", result.err, result.out)
				}
				release()
				if out, err := f.run("status"); err == nil {
					t.Fatalf("failed final indexing left a running collector: %s", out)
				}
				if out, err := f.run("ingest"); err != nil {
					t.Fatalf("recover retained raw telemetry: %v\n%s", err, out)
				}
			} else if result.err != nil {
				t.Fatalf("stop did not recover after lock release: %v\n%s", result.err, result.out)
			}
			out, err := f.run("query", "logs", "--service", service, "--since", "1m")
			var rows []json.RawMessage
			if err != nil || json.Unmarshal(out, &rows) != nil || len(rows) != 1 {
				t.Fatalf("shutdown telemetry not queryable: %v\n%s", err, out)
			}
		})
	}
}
