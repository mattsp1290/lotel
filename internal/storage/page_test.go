package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pageDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertPageTrace(t *testing.T, db *sql.DB, name, service string, at time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO traces (trace_id,span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,date) VALUES ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bbbbbbbbbbbbbbbb',?,1,?,?,1,0,?,?)`, name, at, at, service, at.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
}

func insertPageLog(t *testing.T, db *sql.DB, body, service string, at time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO logs (timestamp,severity_number,body,service_name,date) VALUES (?,9,?,?,?)`, at, body, service, at.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestPageSnapshotIgnoresLaterIngestionAndRejectsDeletion(t *testing.T) {
	for _, signal := range []string{"traces", "logs"} {
		t.Run(signal, func(t *testing.T) {
			db := pageDB(t)
			at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
			for i := 0; i < 5; i++ {
				if signal == "traces" {
					insertPageTrace(t, db, "duplicate", "app", at)
				} else {
					insertPageLog(t, db, "duplicate", "app", at)
				}
			}
			request := PageRequest{Signal: signal, Service: "app", Since: at.Add(-time.Hour), Until: at.Add(time.Hour), Limit: 2}
			first, err := QueryPage(db, request)
			if err != nil {
				t.Fatal(err)
			}
			if first.NextCursor == "" {
				t.Fatal("missing continuation")
			}
			if signal == "traces" {
				insertPageTrace(t, db, "later", "app", at.Add(-time.Minute))
			} else {
				insertPageLog(t, db, "later", "app", at.Add(-time.Minute))
			}
			second, err := QueryPage(db, PageRequest{Signal: signal, Cursor: first.NextCursor})
			if err != nil {
				t.Fatal(err)
			}
			if second.NextCursor == "" {
				t.Fatal("missing second continuation")
			}
			third, err := QueryPage(db, PageRequest{Signal: signal, Cursor: second.NextCursor})
			if err != nil {
				t.Fatal(err)
			}
			if third.NextCursor != "" {
				t.Fatal("later insertion entered snapshot")
			}
			counts := []int{}
			for _, p := range []PageResult{first, second, third} {
				if signal == "traces" {
					rows := p.Records.([]TraceResult)
					counts = append(counts, len(rows))
					for _, r := range rows {
						if r.Name != "duplicate" {
							t.Fatal("unexpected row", r.Name)
						}
					}
				} else {
					rows := p.Records.([]LogResult)
					counts = append(counts, len(rows))
					for _, r := range rows {
						if r.Body != "duplicate" {
							t.Fatal("unexpected row", r.Body)
						}
					}
				}
			}
			if len(counts) != 3 || counts[0] != 2 || counts[1] != 2 || counts[2] != 1 {
				t.Fatalf("counts %v", counts)
			}
			if _, err := db.Exec("DELETE FROM " + signal + " WHERE cursor_id=1"); err != nil {
				t.Fatal(err)
			}
			if _, err := QueryPage(db, PageRequest{Signal: signal, Cursor: first.NextCursor}); err == nil || !strings.Contains(err.Error(), "invalidated") {
				t.Fatalf("deletion: %v", err)
			}
		})
	}
}

func TestPageCursorIntegrityAndIsolation(t *testing.T) {
	db := pageDB(t)
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	insertPageTrace(t, db, "one", "app", at)
	insertPageTrace(t, db, "two", "app", at)
	first, err := QueryPage(db, PageRequest{Signal: "traces", Since: at.Add(-time.Hour), Until: at.Add(time.Hour), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := QueryPage(db, PageRequest{Signal: "logs", Cursor: first.NextCursor}); err == nil {
		t.Fatal("wrong signal accepted")
	}
	if _, err := QueryPage(db, PageRequest{Signal: "traces", Cursor: first.NextCursor, Service: "other"}); err == nil {
		t.Fatal("additional filter accepted")
	}
	modified := []byte(first.NextCursor)
	if modified[0] == 'A' {
		modified[0] = 'B'
	} else {
		modified[0] = 'A'
	}
	if _, err := QueryPage(db, PageRequest{Signal: "traces", Cursor: string(modified)}); err == nil {
		t.Fatal("modified cursor accepted")
	}
	other := pageDB(t)
	if _, err := QueryPage(other, PageRequest{Signal: "traces", Since: at.Add(-time.Hour), Until: at.Add(time.Hour), Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryPage(other, PageRequest{Signal: "traces", Cursor: first.NextCursor}); err == nil {
		t.Fatal("cursor accepted on another database")
	}
}

func TestPageMigrationRollsBackOnCollision(t *testing.T) {
	db := pageDB(t)
	if _, err := db.Exec(`CREATE SEQUENCE lotel_log_cursor_seq`); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	if _, err := QueryPage(db, PageRequest{Signal: "traces", Since: at, Until: at, Limit: 1}); err == nil {
		t.Fatal("expected migration failure")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name IN ('traces','logs') AND column_name='cursor_id'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial columns: %d", count)
	}
}

func TestPageMigrationDoesNotChangeRecentOrdering(t *testing.T) {
	for _, signal := range []string{"traces", "logs"} {
		t.Run(signal, func(t *testing.T) {
			db := pageDB(t)
			at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
			for _, attrs := range []string{`{"v":0}`, `{"v":-0}`} {
				var err error
				if signal == "traces" {
					_, err = db.Exec(`INSERT INTO traces (trace_id,span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,attributes,date) VALUES ('a','b','n',1,?,?,0,0,'app',?,'2025-01-02')`, at, at, attrs)
				} else {
					_, err = db.Exec(`INSERT INTO logs (timestamp,severity_number,body,service_name,attributes,date) VALUES (?,9,'body','app',?,'2025-01-02')`, at, attrs)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			opts := QueryOptions{Since: at.Add(-time.Second), Until: at.Add(time.Second), Limit: 1}
			var before, after []byte
			if signal == "traces" {
				rows, _, err := RecentTraces(db, TraceQueryOptions{QueryOptions: opts})
				if err != nil {
					t.Fatal(err)
				}
				before, err = json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				rows, _, err := RecentLogs(db, LogQueryOptions{QueryOptions: opts})
				if err != nil {
					t.Fatal(err)
				}
				before, err = json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := QueryPage(db, PageRequest{Signal: signal, Since: opts.Since, Until: opts.Until, Limit: 1}); err != nil {
				t.Fatal(err)
			}
			if signal == "traces" {
				rows, _, err := RecentTraces(db, TraceQueryOptions{QueryOptions: opts})
				if err != nil {
					t.Fatal(err)
				}
				after, err = json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				rows, _, err := RecentLogs(db, LogQueryOptions{QueryOptions: opts})
				if err != nil {
					t.Fatal(err)
				}
				after, err = json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
			}
			if string(before) != string(after) {
				t.Fatalf("ordering changed: before %s after %s", before, after)
			}
		})
	}
}

func TestPagePruneAndReingestInvalidation(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "traces.jsonl"), append(append([]byte{}, fixture...), fixture...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	first, err := QueryPage(db, PageRequest{Signal: "traces", Since: start, Until: end, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == "" {
		t.Fatal("fixture requires multiple spans")
	}
	if _, err := Prune(db, end, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(db, start, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryPage(db, PageRequest{Signal: "traces", Cursor: first.NextCursor}); err != nil {
		t.Fatalf("no-op prune invalidated cursor: %v", err)
	}
	if _, err := ReingestAll(db, root); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryPage(db, PageRequest{Signal: "traces", Cursor: first.NextCursor}); err == nil || !strings.Contains(err.Error(), "invalidated") {
		t.Fatalf("rebuild did not invalidate cursor: %v", err)
	}
}

func TestPageCursorSurvivesDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lotel.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	insertPageLog(t, db, "one", "app", at)
	insertPageLog(t, db, "two", "app", at)
	first, err := QueryPage(db, PageRequest{Signal: "logs", Since: at.Add(-time.Hour), Until: at.Add(time.Hour), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertPageLog(t, db, "new", "app", at)
	next, err := QueryPage(db, PageRequest{Signal: "logs", Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	rows := next.Records.([]LogResult)
	if len(rows) != 1 || rows[0].Body == "new" || next.NextCursor != "" {
		t.Fatalf("unexpected continuation: %+v", next)
	}
}

func TestPageEscapedServiceCursorFitsLimit(t *testing.T) {
	db := pageDB(t)
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	service := strings.Repeat("\x01", 1024)
	insertPageLog(t, db, "one", service, at)
	insertPageLog(t, db, "two", service, at)
	first, err := QueryPage(db, PageRequest{Signal: "logs", Service: service, Since: at.Add(-time.Second), Until: at.Add(time.Second), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.NextCursor) > maxPageToken {
		t.Fatalf("cursor too long: %d", len(first.NextCursor))
	}
	if _, err := QueryPage(db, PageRequest{Signal: "logs", Cursor: first.NextCursor}); err != nil {
		t.Fatal(err)
	}
}
