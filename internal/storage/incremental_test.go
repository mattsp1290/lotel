package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIngestFileEdges(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "genai", "traces.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"append", "partial", "replace", "malformed", "long"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			root := t.TempDir()
			dir := filepath.Join(root, "traces")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "traces.jsonl")
			first := fixture
			switch scenario {
			case "partial":
				first = bytes.TrimSuffix(fixture, []byte("\n"))
			case "malformed":
				first = append(append(append([]byte{}, fixture...), []byte("bad json\n")...), fixture...)
			case "long":
				first = append(append(append([]byte{}, fixture[:len(fixture)-1]...), bytes.Repeat([]byte(" "), 11<<20)...), '\n')
			}
			if err := os.WriteFile(path, first, 0600); err != nil {
				t.Fatal(err)
			}
			rep, err := IngestAll(db, root)
			if err != nil {
				t.Fatal(err)
			}
			want := 4
			if scenario == "partial" {
				want = 0
			}
			if scenario == "malformed" {
				want = 8
				if rep.Signals["traces"].SkippedLines != 1 {
					t.Fatal("malformed line not counted")
				}
			}
			if rep.Signals["traces"].Records != want {
				t.Fatalf("first insert: %d want %d", rep.Signals["traces"].Records, want)
			}
			switch scenario {
			case "append", "partial":
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				tail := fixture
				if scenario == "partial" {
					tail = []byte("\n")
				}
				if _, err := file.Write(tail); err != nil {
					t.Fatal(err)
				}
				file.Close()
			case "replace":
				replacement := bytes.ReplaceAll(fixture, []byte("fixture-model"), []byte("changed-model"))
				if err := os.WriteFile(path, replacement, 0600); err != nil {
					t.Fatal(err)
				}
			}
			rep, err = IngestAll(db, root)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if scenario == "append" || scenario == "partial" || scenario == "replace" {
				expected = 4
			}
			if rep.Signals["traces"].Records != expected {
				t.Fatalf("tail insert: %d want %d", rep.Signals["traces"].Records, expected)
			}
			rep, err = IngestAll(db, root)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Signals["traces"].Records != 0 {
				t.Fatal("tail duplicated")
			}
		})
	}
}

func TestIngestNonFiniteDoubles(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := filepath.Join(t.TempDir(), "metrics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{"name":"nan","gauge":{"dataPoints":[{"timeUnixNano":"1794700000000000000","asDouble":"NaN"}]}},{"name":"finite","sum":{"dataPoints":[{"timeUnixNano":"1794700000000000000","asDouble":3}]}}]}]}]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	rep, err := IngestAll(db, filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Signals["metrics"].Records != 2 || rep.Signals["metrics"].SkippedLines != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	rows, err := QueryMetrics(db, QueryOptions{Name: "nan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Value != nil {
		t.Fatal("non-finite value did not become null")
	}
}
