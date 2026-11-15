package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fullOTLPFixtures() map[string]string {
	return map[string]string{
		"traces":   `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"full-otlp"}}]},"scopeSpans":[{"spans":[{"traceId":"11111111111111111111111111111111","spanId":"1111111111111111","name":"full trace","startTimeUnixNano":"1794700000000000001","endTimeUnixNano":"1794700000000000002"}]}]}]}`,
		"metrics":  `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"full-otlp"}}]},"scopeMetrics":[{"metrics":[{"name":"full.metric","gauge":{"dataPoints":[{"timeUnixNano":"1794700000000000001","asInt":"9007199254740993"}]}}]}]}]}`,
		"logs":     `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"full-otlp"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000001","body":{"stringValue":"full log"}}]}]}]}`,
		"profiles": `{"resourceProfiles":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"full-otlp"}}]},"scopeProfiles":[{"profiles":[{"profileId":"22222222222222222222222222222222","timeUnixNano":"1794700000000000001","durationNano":"8","sampleType":{"typeStrindex":1,"unitStrindex":2},"samples":[{"stackIndex":0,"values":["9007199254740993"],"timestampsUnixNano":["1794700000000000003"]}]}]}]}],"dictionary":{"stringTable":["","cpu","nanoseconds"],"stackTable":[{"locationIndices":[]}]}}`,
	}
}

func TestReingestAllPreservesFourSignalsWithoutDuplicates(t *testing.T) {
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
	if _, err := db.Exec("DELETE FROM otlp_envelopes"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for signal, count := range unchanged.Signals {
		if count.Records != 0 || count.OTLPEnvelopes != 0 {
			t.Fatalf("retained cursors replayed %s before full rebuilding: %+v", signal, count)
		}
	}
	for range 2 {
		report, err := ReingestAll(db, root)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Signals) != 4 {
			t.Fatalf("full rebuild reported %d signals, want 4", len(report.Signals))
		}
		for signal, payload := range fixtures {
			if report.Signals[signal].Records != 1 || report.Signals[signal].OTLPEnvelopes != 1 {
				t.Fatalf("full rebuild report for %s: %+v", signal, report.Signals[signal])
			}
			rows, err := QueryOTLP(db, signal, QueryOptions{Service: "full-otlp"})
			if err != nil || len(rows) != 1 {
				t.Fatalf("rebuilt %s envelopes: %d, %v", signal, len(rows), err)
			}
			assertSameJSON(t, []byte(payload), rows[0].Payload)
		}
		for table := range ingestStatements {
			var rows int
			if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&rows); err != nil || rows != 1 {
				t.Fatalf("rebuilt %s projections: %d, %v", table, rows, err)
			}
		}
		var cursorCount int
		if err := db.QueryRow("SELECT count(*) FROM ingest_state").Scan(&cursorCount); err != nil || cursorCount != 4 {
			t.Fatalf("full rebuild retained %d cursors, want 4: %v", cursorCount, err)
		}
	}
	report, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	for signal, count := range report.Signals {
		if count.Records != 0 || count.OTLPEnvelopes != 0 {
			t.Fatalf("incremental ingestion duplicated %s after full rebuild: %+v", signal, count)
		}
	}
}

func TestReingestAllRollsBackEnvelopesAndAllCursors(t *testing.T) {
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
	var beforeCursors string
	cursors := `SELECT string_agg(path || ':' || byte_offset || ':' || fingerprint || ':' || fingerprint_len || ':' || updated_at, ',' ORDER BY path) FROM ingest_state`
	if err := db.QueryRow(cursors).Scan(&beforeCursors); err != nil {
		t.Fatal(err)
	}
	writeOTLPSignal(t, root, "logs", strings.Replace(fixtures["logs"], "full log", "uncommitted log", 1)+"\n")
	profilePath := filepath.Join(root, "profiles", "profiles.jsonl")
	if err := os.Remove(profilePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(profilePath, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReingestAll(db, root); err == nil || !strings.Contains(err.Error(), "reingesting profiles") {
		t.Fatalf("full rebuild did not report its unreadable final signal: %v", err)
	}
	var afterCursors string
	if err := db.QueryRow(cursors).Scan(&afterCursors); err != nil || afterCursors != beforeCursors {
		t.Fatalf("failed full rebuild changed cursors: %v\nwant %s\ngot %s", err, beforeCursors, afterCursors)
	}
	for signal, payload := range fixtures {
		rows, err := QueryOTLP(db, signal, QueryOptions{Service: "full-otlp"})
		if err != nil || len(rows) != 1 {
			t.Fatalf("failed full rebuild changed %s envelopes: %d, %v", signal, len(rows), err)
		}
		assertSameJSON(t, []byte(payload), rows[0].Payload)
	}
	for table := range ingestStatements {
		var rows int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("failed full rebuild changed %s rows: %d, %v", table, rows, err)
		}
	}
}
