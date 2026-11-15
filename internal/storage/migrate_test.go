package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrate_FreshCreatesV3(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if SchemaRebuilt {
		t.Fatal("fresh database reported rebuild")
	}
	var version string
	if err := db.QueryRow("SELECT value FROM schema_meta WHERE key='schema_version'").Scan(&version); err != nil || version != "3" {
		t.Fatalf("version %q: %v", version, err)
	}
	_, err = db.Exec(`INSERT INTO metrics(metric_name,metric_type,timestamp,service_name,date,"count","sum","min","max") VALUES ('tokens','histogram',CURRENT_TIMESTAMP,'test',CURRENT_DATE,3,6,1,3)`)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	var sum, min, max float64
	if err := db.QueryRow(`SELECT "count","sum","min","max" FROM metrics`).Scan(&count, &sum, &min, &max); err != nil {
		t.Fatal(err)
	}
	if count != 3 || sum != 6 || min != 1 || max != 3 {
		t.Fatal("metric statistics changed")
	}
}

func TestMigrate_RebuildsV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	old, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("CREATE TABLE traces(trace_id VARCHAR); INSERT INTO traces VALUES ('old')"); err != nil {
		t.Fatal(err)
	}
	old.Close()
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !SchemaRebuilt {
		t.Fatal("missing rebuild report")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM traces").Scan(&count); err != nil || count != 0 {
		t.Fatalf("old rows remain: %d %v", count, err)
	}
}

func TestMigrate_RejectsNewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_meta SET value='4'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenDB(path)
	if db != nil {
		db.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("unexpected error: %v", err)
	}
}
