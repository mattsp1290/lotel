//go:build !windows

package collector_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
)

func TestNativeStartupSurvivesIngestionError(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	configPath := filepath.Join(f.dir, "lotel-collector.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(config), "interval: 1h", "interval: 100ms", 1)
	text = strings.Replace(text, "timeout: 5s", "timeout: 100ms", 1)
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(f.home, ".lotel", "data", "lotel.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, []byte("corrupt retained index\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start", "--backend", "native"); err != nil {
		t.Fatalf("indexing error stopped collection: %v\n%s", err, out)
	}
	status := func() collector.Status {
		t.Helper()
		out, err := f.run("status")
		var result collector.Status
		if err != nil || json.Unmarshal(out, &result) != nil || !result.Running {
			t.Fatalf("running status: %v\n%s", err, out)
		}
		return result
	}
	before := status()
	deadline := time.Now().Add(5 * time.Second)
	for before.Ingestion.LastError == "" {
		if time.Now().After(deadline) {
			t.Fatal("status did not expose the indexing error")
		}
		time.Sleep(25 * time.Millisecond)
		before = status()
	}
	if before.Healthy || !before.Ingestion.Running || before.PID == 0 {
		t.Fatalf("indexing error lost health or process state: %+v", before)
	}
	if out, err := f.run("health"); err == nil {
		t.Fatalf("health hid the indexing error: %s", out)
	}
	if out, err := f.run("start", "--backend", "native", "--wait"); err == nil || !strings.Contains(string(out), "did not become healthy") {
		t.Fatalf("wait did not report the indexing failure: %v\n%s", err, out)
	}
	if after := status(); after.PID != before.PID || after.Healthy {
		t.Fatalf("failed health wait changed collection: %+v -> %+v", before, after)
	}
	postUpgradeLog(t, f, "collected while index unavailable")
	deadline = time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(dbPath), "logs", "logs.jsonl"))
		if err == nil && strings.Contains(string(raw), "collected while index unavailable") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Collector did not retain raw telemetry: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	other := f
	other.home = t.TempDir()
	other.env = append(append([]string{}, f.env...), "HOME="+other.home)
	if out, err := other.run("start", "--backend", "native"); err == nil {
		other.run("stop")
		t.Fatalf("occupied receiver ports accepted startup: %s", out)
	}
	if _, err := os.Stat(filepath.Join(other.home, ".lotel", "collector.state")); !os.IsNotExist(err) {
		t.Fatalf("failed occupied-port startup persisted state: %v", err)
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		after := status()
		if after.PID != before.PID {
			t.Fatalf("index repair restarted Collector: %d -> %d", before.PID, after.PID)
		}
		if after.Healthy && after.Ingestion.LastError == "" && after.Ingestion.LastSuccess != nil {
			out, err := f.run("query", "logs", "--service", "native-upgrade-test", "--since", "1m")
			var rows []json.RawMessage
			if err == nil && json.Unmarshal(out, &rows) == nil && len(rows) == 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic ingestion did not recover: %+v", after)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if out, err := f.run("health"); err != nil {
		t.Fatalf("health did not recover: %v\n%s", err, out)
	}
}
