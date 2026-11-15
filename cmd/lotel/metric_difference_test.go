package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsp1290/lotel/internal/storage"
)

const differenceSelector = `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"cli-test"}}]},"scopeMetrics":[{"scope":{"name":"sdk"},"metrics":[{"name":"requests","unit":"1","sum":{"aggregationTemporality":2,"isMonotonic":true,"dataPoints":[{"attributes":[]}]}}]}]}]}`
const differenceData = `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"cli-test"}}]},"scopeMetrics":[{"scope":{"name":"sdk"},"metrics":[{"name":"requests","unit":"1","sum":{"aggregationTemporality":2,"isMonotonic":true,"dataPoints":[{"timeUnixNano":"1700000000000000001","startTimeUnixNano":"1600000000000000000","asInt":"9007199254740993"},{"timeUnixNano":"1700000000000000002","startTimeUnixNano":"1600000000000000000","asInt":"9007199254741000"}]}}]}]}]}`

func invokeMetricDifference(t *testing.T, home string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--", "query", "metric-difference"}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestMetricDifferenceCLI(t *testing.T) {
	home := t.TempDir()
	selector := filepath.Join(t.TempDir(), "selector.json")
	if err := os.WriteFile(selector, []byte(differenceSelector), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--selector", selector, "--from-ns", "1700000000000000001", "--to-ns", "1700000000000000002"}
	for _, bad := range [][]string{
		{"--selector", selector, "--from-ns", "bad", "--to-ns", "2"},
		{"--selector", selector, "--from-ns", "2", "--to-ns", "1"},
		{"--selector", filepath.Join(home, "missing"), "--from-ns", "1", "--to-ns", "2"},
	} {
		out, errText, err := invokeMetricDifference(t, home, bad...)
		if err == nil || out != "" || errText == "" {
			t.Fatalf("invalid args accepted: %q %q %v", out, errText, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
			t.Fatalf("invalid arguments opened database: %v", err)
		}
	}
	badSelector := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badSelector, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invokeMetricDifference(t, home, "--selector", badSelector, "--from-ns", "1", "--to-ns", "2")
	if err == nil || out != "" || !strings.Contains(stderr, "selector") {
		t.Fatalf("invalid selector: %q %q %v", out, stderr, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("invalid selector opened database: %v", err)
	}
	out, stderr, err = invokeMetricDifference(t, home, args...)
	if err == nil || out != "" || !strings.Contains(stderr, "indeterminate") {
		t.Fatalf("missing evidence: %q %q %v", out, stderr, err)
	}
	dataDir := filepath.Join(home, ".lotel", "data")
	metricDir := filepath.Join(dataDir, "metrics")
	if err := os.MkdirAll(metricDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metricDir, "metrics.jsonl"), []byte(differenceData+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(dataDir, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.IngestAll(db, dataDir); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	out, stderr, err = invokeMetricDifference(t, home, args...)
	if err != nil {
		t.Fatalf("query failed: %q %q %v", out, stderr, err)
	}
	var result storage.MetricDifference
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Difference != "7" || result.ElapsedNano != "1" || len(result.Assumptions) != 2 {
		t.Fatalf("unexpected result: %s", out)
	}
}
