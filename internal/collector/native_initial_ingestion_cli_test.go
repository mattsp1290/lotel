//go:build !windows

package collector_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
)

func TestNativeInitialIngestionReadiness(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "enabled"
		if disabled {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			f := newNativeCLIFixture(t, "")
			configPath := filepath.Join(f.dir, "lotel-collector.yaml")
			config, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(config), "timeout: 5s", "timeout: 100ms", 1)
			if disabled {
				text = strings.Replace(text, "enabled: true", "enabled: false", 1)
			}
			if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			dataPath := filepath.Join(f.home, ".lotel", "data")
			if err := os.MkdirAll(dataPath, 0o700); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(filepath.Join(dataPath, "lotel.db.lock"), os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { lock.Close() })
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			status := func() collector.Status {
				t.Helper()
				out, err := f.run("status")
				var result collector.Status
				if err != nil || json.Unmarshal(out, &result) != nil || !result.Running || result.PID == 0 {
					t.Fatalf("running status: %v\n%s", err, out)
				}
				return result
			}
			if disabled {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if out, err := f.command(ctx, "start", "--backend", "native", "--wait").CombinedOutput(); err != nil {
					t.Fatalf("disabled ingestion blocked readiness: %v\n%s", err, out)
				}
				after := status()
				if !after.Healthy || after.Ingestion.Enabled || after.Ingestion.Running || after.Ingestion.LastSuccess != nil {
					t.Fatalf("disabled ingestion required indexing: %+v", after)
				}
				if out, err := f.run("health"); err != nil {
					t.Fatalf("disabled ingestion health: %v\n%s", err, out)
				}
				return
			}
			if out, err := f.run("start", "--backend", "native"); err != nil {
				t.Fatalf("blocked indexing prevented collection: %v\n%s", err, out)
			}
			before := status()
			if before.Healthy || !before.Ingestion.Running || before.Ingestion.LastSuccess != nil || before.Ingestion.LastError != "" {
				t.Fatalf("blocked initial ingestion reported ready: %+v", before)
			}
			if out, err := f.run("health"); err == nil {
				t.Fatalf("health succeeded before first ingestion: %s", out)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			type waitResult struct {
				output []byte
				err    error
			}
			done := make(chan waitResult, 1)
			go func() {
				out, err := f.command(ctx, "start", "--backend", "native", "--wait").CombinedOutput()
				done <- waitResult{out, err}
			}()
			select {
			case result := <-done:
				t.Fatalf("wait completed before first ingestion: %v\n%s", result.err, result.output)
			case <-time.After(time.Second):
			}
			postUpgradeLog(t, f, "collected before initial indexing")
			deadline := time.Now().Add(5 * time.Second)
			for {
				raw, err := os.ReadFile(filepath.Join(dataPath, "logs", "logs.jsonl"))
				if err == nil && strings.Contains(string(raw), "collected before initial indexing") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("Collector did not retain raw telemetry during blocked indexing: %v", err)
				}
				time.Sleep(25 * time.Millisecond)
			}
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			result := <-done
			if result.err != nil {
				t.Fatalf("wait did not recover after releasing database lock: %v\n%s", result.err, result.output)
			}
			after := status()
			if !after.Healthy || after.PID != before.PID || after.Ingestion.LastSuccess == nil || after.Ingestion.LastError != "" {
				t.Fatalf("initial ingestion recovery changed collection: %+v -> %+v", before, after)
			}
			if out, err := f.run("health"); err != nil {
				t.Fatalf("health did not recover: %v\n%s", err, out)
			}
			out, err := f.run("query", "logs", "--service", "native-upgrade-test", "--since", "1m")
			var rows []json.RawMessage
			if err != nil || json.Unmarshal(out, &rows) != nil || len(rows) != 1 || !strings.Contains(string(out), "collected before initial indexing") {
				t.Fatalf("initial ingestion did not index retained telemetry: %v\n%s", err, out)
			}
		})
	}
}
