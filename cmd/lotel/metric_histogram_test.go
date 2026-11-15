package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestMetricHistogramCLI(t *testing.T) {
	const envelope = `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{"name":"latency","unit":"ms","histogram":{"aggregationTemporality":%d,"dataPoints":[%s]}}]}]}]}`
	const first = `{"startTimeUnixNano":"10","timeUnixNano":"20","count":"3","bucketCounts":["1","2"],"explicitBounds":[5]}`
	for _, tc := range []struct {
		command, basis, last, from, count string
		temporality                       int
		buckets                           []string
	}{
		{"metric-delta-histogram", "contiguous_delta_bucket_counts", `{"startTimeUnixNano":"20","timeUnixNano":"30","count":"2","bucketCounts":["0","2"],"explicitBounds":[5]}`, "10", "5", 1, []string{"1", "4"}},
		{"metric-cumulative-histogram", "cumulative_bucket_count_difference", `{"startTimeUnixNano":"10","timeUnixNano":"30","count":"8","bucketCounts":["1","7"],"explicitBounds":[5]}`, "20", "5", 2, []string{"0", "5"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			home := t.TempDir()
			selector := filepath.Join(t.TempDir(), "selector.json")
			selectorData := fmt.Sprintf(envelope, tc.temporality, "{}")
			data := fmt.Sprintf(envelope, tc.temporality, first+","+tc.last)
			if err := os.WriteFile(selector, []byte(selectorData), 0600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(home, ".lotel", "data")
			if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0700); err != nil {
				t.Fatal(err)
			}
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
				cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelper$", "--", "query", tc.command, "--selector", selector, "--from-ns", from, "--to-ns", "30")
				cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				return stdout.String(), stderr.String(), err
			}
			out, stderr, err := run(tc.from)
			if err != nil {
				t.Fatalf("query failed: %q %q %v", out, stderr, err)
			}
			validateOutput(t, "query "+tc.command, []byte(out))
			var got storage.MetricHistogram
			if err := json.Unmarshal([]byte(out), &got); err != nil || got.Count != tc.count || got.PointCount != 2 || !slices.Equal(got.BucketCounts, tc.buckets) || got.Basis != tc.basis || !strings.Contains(got.ValidationScope, "Sum, min and max are ignored") {
				t.Fatalf("unexpected result: %q %v", out, err)
			}
			out, stderr, err = run("9")
			if err == nil || out != "" || !strings.Contains(stderr, "indeterminate") {
				t.Fatalf("gap accepted: %q %q %v", out, stderr, err)
			}
		})
	}
}
