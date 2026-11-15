package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestMetricDifferenceWaitsForDatabaseLock(t *testing.T) {
	home := t.TempDir()
	selector := filepath.Join(t.TempDir(), "selector.json")
	if err := os.WriteFile(selector, []byte(differenceSelector), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(home, ".lotel", "data")
	metricDir := filepath.Join(dataDir, "metrics")
	if err := os.MkdirAll(metricDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metricDir, "metrics.jsonl"), []byte(differenceData+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "lotel.db")
	if err := storage.WithDB(context.Background(), path, func(db *sql.DB) error {
		_, err := storage.IngestAll(db, dataDir)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- storage.WithDB(context.Background(), path, func(*sql.DB) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIHelper$", "--", "query", "metric-difference", "--selector", selector, "--from-ns", "1700000000000000001", "--to-ns", "1700000000000000002")
	cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		close(release)
		<-holderDone
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		close(release)
		<-holderDone
		t.Fatalf("command finished while database was locked: %v, %s", err, stderr.String())
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("command did not finish after lock release: %v, %s", err, stderr.String())
	}
	var result storage.MetricDifference
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Difference != "7" {
		t.Fatalf("unexpected result after lock release: %s (%v)", stdout.String(), err)
	}
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

type blockingMetricWriter struct {
	started chan struct{}
	release chan struct{}
}

func (w *blockingMetricWriter) Write(p []byte) (int, error) {
	close(w.started)
	<-w.release
	return len(p), nil
}

func TestMetricDifferenceReleasesLockBeforeOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	selector := filepath.Join(t.TempDir(), "selector.json")
	if err := os.WriteFile(selector, []byte(differenceSelector), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(home, ".lotel", "data")
	metricDir := filepath.Join(dataDir, "metrics")
	if err := os.MkdirAll(metricDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metricDir, "metrics.jsonl"), []byte(differenceData+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "lotel.db")
	if err := storage.WithDB(context.Background(), path, func(db *sql.DB) error { _, err := storage.IngestAll(db, dataDir); return err }); err != nil {
		t.Fatal(err)
	}
	writer := &blockingMetricWriter{started: make(chan struct{}), release: make(chan struct{})}
	cmd := metricDifferenceCommand()
	cmd.SetOut(writer)
	cmd.SetArgs([]string{"--selector", selector, "--from-ns", "1700000000000000001", "--to-ns", "1700000000000000002"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-writer.started:
	case err := <-done:
		t.Fatalf("command finished before writing: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not reach output")
	}
	released := false
	defer func() {
		if !released {
			close(writer.release)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := storage.WithDB(ctx, path, func(*sql.DB) error { return nil }); err != nil {
		t.Fatalf("database remained locked while output blocked: %v", err)
	}
	close(writer.release)
	released = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMetricDifferenceCLIRejectsLaterOverlappingEpoch(t *testing.T) {
	home := t.TempDir()
	selector := filepath.Join(t.TempDir(), "selector.json")
	if err := os.WriteFile(selector, []byte(differenceSelector), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(home, ".lotel", "data")
	dir := filepath.Join(dataDir, "metrics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	later := `"asInt":"9007199254741000"},{"timeUnixNano":"1700000000000000003","startTimeUnixNano":"1700000000000000001","asInt":"1"}]`
	data := strings.Replace(differenceData, `"asInt":"9007199254741000"}]`, later, 1)
	if data == differenceData {
		t.Fatal("fixture replacement failed")
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(data+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "lotel.db")
	if err := storage.WithDB(context.Background(), path, func(db *sql.DB) error { _, err := storage.IngestAll(db, dataDir); return err }); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invokeMetricDifference(t, home, "--selector", selector, "--from-ns", "1700000000000000001", "--to-ns", "1700000000000000002")
	if err == nil || out != "" || !strings.Contains(stderr, "overlapping cumulative epoch") {
		t.Fatalf("overlap accepted: %q %q %v", out, stderr, err)
	}
}

func TestMetricDifferenceCLIRejectsOversizedSelectorBeforeDatabase(t *testing.T) {
	home := t.TempDir()
	selector := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(selector, bytes.Repeat([]byte(" "), storage.MetricSelectorByteLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invokeMetricDifference(t, home, "--selector", selector, "--from-ns", "1", "--to-ns", "2")
	if err == nil || out != "" || !strings.Contains(stderr, "size limit") {
		t.Fatalf("oversized selector accepted: %q %q %v", out, stderr, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("invalid selector opened database: %v", err)
	}
}

func TestMetricDifferenceCLISeparatesDisjointAndAmbiguousResources(t *testing.T) {
	selector := filepath.Join(t.TempDir(), "selector.json")
	if err := os.WriteFile(selector, []byte(differenceSelector), 0600); err != nil {
		t.Fatal(err)
	}
	incomplete := strings.Replace(differenceData, `"resource":{"attributes":`, `"resource":{"droppedAttributesCount":1,"attributes":`, 1)
	other := strings.Replace(incomplete, `"stringValue":"cli-test"`, `"stringValue":"other"`, 1)
	other = strings.Replace(other, `"name":"requests"`, `"name":"requests","newField":42`, 1)
	for _, tc := range []struct {
		name, candidate string
		wantSuccess     bool
	}{
		{"disjoint incomplete resource and unknown metric", other, true},
		{"possibly matching incomplete resource", incomplete, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dataDir := filepath.Join(home, ".lotel", "data")
			dir := filepath.Join(dataDir, "metrics")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(tc.candidate+"\n"+differenceData+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := storage.WithDB(context.Background(), filepath.Join(dataDir, "lotel.db"), func(db *sql.DB) error { _, err := storage.IngestAll(db, dataDir); return err }); err != nil {
				t.Fatal(err)
			}
			out, stderr, err := invokeMetricDifference(t, home, "--selector", selector, "--from-ns", "1700000000000000001", "--to-ns", "1700000000000000002")
			if tc.wantSuccess {
				var result storage.MetricDifference
				if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Difference != "7" {
					t.Fatalf("disjoint resource blocked: %q %q %v", out, stderr, err)
				}
			} else if err == nil || out != "" || !strings.Contains(stderr, "indeterminate") {
				t.Fatalf("ambiguous resource accepted: %q %q %v", out, stderr, err)
			}
		})
	}
}
