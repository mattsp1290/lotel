package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReingestAllReplacesRowsAndResetsCursors(t *testing.T) {
	root := t.TempDir()
	db, err := OpenDB(filepath.Join(root, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for signal := range ingestStatements {
		fixture, err := os.ReadFile(filepath.Join("testdata", "genai", signal+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, signal)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, signal+".jsonl"), fixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for signal := range ingestStatements {
		if _, err := db.Exec("DELETE FROM " + signal); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	for signal := range ingestStatements {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM " + signal).Scan(&count); err != nil || count != 0 {
			t.Fatalf("incremental ingestion restored %s rows without new bytes: %d, %v", signal, count, err)
		}
	}
	for range 2 {
		report, err := ReingestAll(db, root)
		if err != nil {
			t.Fatal(err)
		}
		for signal := range ingestStatements {
			want := first.Signals[signal].Records
			var count int
			if err := db.QueryRow("SELECT count(*) FROM " + signal).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != want || report.Signals[signal].Records != want {
				t.Fatalf("rebuilt %s has %d rows, report has %d, want %d", signal, count, report.Signals[signal].Records, want)
			}
		}
	}
	report, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for signal := range ingestStatements {
		if report.Signals[signal].Records != 0 {
			t.Fatalf("incremental ingestion duplicated rebuilt %s", signal)
		}
	}
	if err := os.Remove(filepath.Join(root, "metrics", "metrics.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReingestAll(db, root); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRow("SELECT count(*) FROM metrics").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("full ingestion retained stale metrics: %d, %v", remaining, err)
	}
}

func TestReingestAllRollsBackRowsAndCursors(t *testing.T) {
	root := t.TempDir()
	db, err := OpenDB(filepath.Join(root, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture, err := os.ReadFile(filepath.Join("testdata", "genai", "traces.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "traces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "traces.jsonl")
	if err := os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE UNIQUE INDEX unique_span ON traces(trace_id,span_id)"); err != nil {
		t.Fatal(err)
	}
	var beforeCount int
	var beforeOffset int64
	if err := db.QueryRow("SELECT count(*) FROM traces").Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT byte_offset FROM ingest_state WHERE path=?", path).Scan(&beforeOffset); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat(fixture, 2), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReingestAll(db, root); err == nil {
		t.Fatal("duplicate spans unexpectedly succeeded")
	}
	var afterCount int
	var afterOffset int64
	if err := db.QueryRow("SELECT count(*) FROM traces").Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT byte_offset FROM ingest_state WHERE path=?", path).Scan(&afterOffset); err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount || afterOffset != beforeOffset {
		t.Fatalf("failed rebuild changed rows or cursor: count %d to %d, offset %d to %d", beforeCount, afterCount, beforeOffset, afterOffset)
	}
}

func TestReingestAllIncludesCompleteOTLPAndProfiles(t *testing.T) {
	root := t.TempDir()
	db, err := OpenDB(filepath.Join(root, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload := `{"resourceProfiles":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"rebuilt-profiles"}}]},"scopeProfiles":[{"profiles":[{"timeUnixNano":"1794700000000000001","durationNano":"8","samples":[{"values":["1"]}]}]}]}]}`
	writeOTLPSignal(t, root, "profiles", payload+"\n")
	for signal := range ingestStatements {
		fixture, err := os.ReadFile(filepath.Join("testdata", "genai", signal+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		writeOTLPSignal(t, root, signal, string(fixture))
	}
	first, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		report, err := ReingestAll(db, root)
		if err != nil {
			t.Fatal(err)
		}
		for _, descriptor := range otlpSignals {
			signal := descriptor.name
			var envelopes int
			if err := db.QueryRow("SELECT count(*) FROM otlp_envelopes WHERE signal=?", signal).Scan(&envelopes); err != nil {
				t.Fatal(err)
			}
			if envelopes != first.Signals[signal].OTLPEnvelopes || envelopes == 0 {
				t.Fatalf("rebuilt %s has %d envelopes, want %d", signal, envelopes, first.Signals[signal].OTLPEnvelopes)
			}
			if report.Signals[signal].Records != first.Signals[signal].Records {
				t.Fatalf("rebuilt %s records changed", signal)
			}
		}
		rows, err := QueryOTLP(db, "profiles", QueryOptions{Service: "rebuilt-profiles"})
		if err != nil || len(rows) != 1 {
			t.Fatalf("profile query after rebuild = %v, %v", rows, err)
		}
		next, err := IngestAll(db, root)
		if err != nil {
			t.Fatal(err)
		}
		for signal, signalReport := range next.Signals {
			if signalReport.Records != 0 || signalReport.OTLPEnvelopes != 0 {
				t.Fatalf("incremental ingestion duplicated rebuilt %s: %+v", signal, signalReport)
			}
		}
	}
}
