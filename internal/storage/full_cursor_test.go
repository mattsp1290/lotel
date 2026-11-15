package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFullRebuildDropsMissingFileCursorAndResumesWhenItReturns(t *testing.T) {
	root := t.TempDir()
	db, err := OpenDB(filepath.Join(root, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixtures := fullOTLPFixtures()
	for signal, payload := range fixtures {
		writeOTLPSignal(t, root, signal, payload+"\n")
	}
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "metrics", "metrics.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	report, err := ReingestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Signals["metrics"].Files != 0 {
		t.Fatal("full rebuild reported the missing file as present")
	}
	var cursors int
	if err := db.QueryRow("SELECT count(*) FROM ingest_state WHERE path=?", path).Scan(&cursors); err != nil || cursors != 0 {
		t.Fatalf("missing file kept a cursor: %d, %v", cursors, err)
	}
	rows, err := QueryOTLP(db, "metrics", QueryOptions{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("missing file kept indexed envelopes: %d, %v", len(rows), err)
	}
	writeOTLPSignal(t, root, "metrics", fixtures["metrics"]+"\n")
	report, err = IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for signal, count := range report.Signals {
		want := 0
		if signal == "metrics" {
			want = 1
		}
		if count.Records != want || count.OTLPEnvelopes != want {
			t.Fatalf("returned file ingestion for %s: %+v, want %d", signal, count, want)
		}
	}
	rows, err = QueryOTLP(db, "metrics", QueryOptions{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("returned file missing from index: %d, %v", len(rows), err)
	}
	assertSameJSON(t, []byte(fixtures["metrics"]), rows[0].Payload)
}

func TestFullRebuildRetainsEmptyAndDictionaryOnlyEnvelopes(t *testing.T) {
	root := t.TempDir()
	db, err := OpenDB(filepath.Join(root, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixtures := map[string]string{
		"traces": "{}", "metrics": "{}", "logs": "{}",
		"profiles": `{"dictionary":{"stringTable":["","retained"]}}`,
	}
	for signal, payload := range fixtures {
		writeOTLPSignal(t, root, signal, payload+"\n")
	}
	for range 2 {
		report, err := ReingestAll(db, root)
		if err != nil {
			t.Fatal(err)
		}
		for signal, payload := range fixtures {
			count := report.Signals[signal]
			if count.Records != 0 || count.OTLPEnvelopes != 1 {
				t.Fatalf("empty %s envelope was dropped: %+v", signal, count)
			}
			rows, err := QueryOTLP(db, signal, QueryOptions{})
			if err != nil || len(rows) != 1 {
				t.Fatalf("empty %s envelope count: %d, %v", signal, len(rows), err)
			}
			assertSameJSON(t, []byte(payload), rows[0].Payload)
		}
	}
	report, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for signal, count := range report.Signals {
		if count.Records != 0 || count.OTLPEnvelopes != 0 {
			t.Fatalf("empty %s envelope duplicated after rebuilding: %+v", signal, count)
		}
	}
}
