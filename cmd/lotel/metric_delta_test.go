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

func TestMetricDeltaTotalCLI(t *testing.T) {
	home := t.TempDir()
	selector := filepath.Join(t.TempDir(), "selector.json")
	selectorData := strings.Replace(differenceSelector, `"aggregationTemporality":2`, `"aggregationTemporality":1`, 1)
	if err := os.WriteFile(selector, []byte(selectorData), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0700); err != nil {
		t.Fatal(err)
	}
	data := strings.Replace(differenceData, `"aggregationTemporality":2`, `"aggregationTemporality":1`, 1)
	data = strings.Replace(data, `"startTimeUnixNano":"1600000000000000000"`, `"startTimeUnixNano":"1700000000000000000"`, 1)
	data = strings.Replace(data, `"startTimeUnixNano":"1600000000000000000"`, `"startTimeUnixNano":"1700000000000000001"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "metrics", "metrics.jsonl"), []byte(data+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(dir, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.IngestAll(db, dir); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	run := func(from string) (string, string, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelper$", "--", "query", "metric-delta-total", "--selector", selector, "--from-ns", from, "--to-ns", "1700000000000000002")
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	out, stderr, err := run("1700000000000000000")
	if err != nil {
		t.Fatalf("query failed: %q %q %v", out, stderr, err)
	}
	validateOutput(t, "query metric-delta-total", []byte(out))
	var got storage.MetricDeltaTotal
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Total != "18014398509481993" || got.RatePerSecond != "9007199254740996500000000" || got.PointCount != 2 {
		t.Fatalf("unexpected result: %q %v", out, err)
	}
	out, stderr, err = run("1699999999999999999")
	if err == nil || out != "" || !strings.Contains(stderr, "indeterminate") {
		t.Fatalf("gap accepted: %q %q %v", out, stderr, err)
	}
}
