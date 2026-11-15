package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func ingestGenAIFixtures(t *testing.T) (*IngestReport, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data")
	for _, signal := range []string{"traces", "metrics", "logs"} {
		content, err := os.ReadFile(filepath.Join("testdata", "genai", signal+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(path, signal)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, signal+".jsonl"), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return nil, path
}

func TestIngestGenAIFixtures(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	report, err := IngestAll(db, path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Signals["traces"].Records != 4 || report.Signals["metrics"].Records != 6 || report.Signals["logs"].Records != 1 {
		t.Fatalf("unexpected report %+v", report)
	}
	traces, err := QueryTraces(db, TraceQueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if traces[0].ScopeName != "fixture" || traces[0].ResourceAttributes["test.resource"] != "present" || len(traces[0].Events) != 1 || len(traces[0].Links) != 1 {
		t.Fatalf("missing trace metadata: %+v", traces[0])
	}
	if _, ok := traces[0].Attributes["gen_ai.input.messages"].(string); !ok {
		t.Fatal("JSON string content changed")
	}
	if _, ok := traces[1].Attributes["gen_ai.output.messages"].([]any); !ok {
		t.Fatal("structured content lost")
	}
	logs, err := QueryLogs(db, LogQueryOptions{QueryOptions: QueryOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	if logs[0].EventName != "gen_ai.client.inference.operation.details" || logs[0].BodyValue == nil {
		t.Fatalf("missing log metadata: %+v", logs[0])
	}
	metrics, err := QueryMetrics(db, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range metrics {
		if m.ScopeName != "fixture" {
			t.Fatal("missing metric scope")
		}
		switch m.MetricType {
		case "histogram":
			if m.Count == nil || *m.Count != 1 || m.Sum == nil || *m.Sum != 5 || len(m.BucketCounts) != len(m.ExplicitBounds)+1 {
				t.Fatalf("histogram lost: %+v", m)
			}
		case "exponential_histogram":
			if m.ExpPositive == nil || len(m.ExpPositive.BucketCounts) != 1 {
				t.Fatal("exponential buckets lost")
			}
		case "summary":
			if len(m.Quantiles) != 1 {
				t.Fatal("quantiles lost")
			}
		}
	}
	again, err := IngestAll(db, path)
	if err != nil {
		t.Fatal(err)
	}
	for signal, rep := range again.Signals {
		if rep.Records != 0 {
			t.Fatalf("%s duplicated", signal)
		}
	}
}
