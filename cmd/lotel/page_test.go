package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestPageCommandContinuesSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, body := range []string{"one", "two", "three"} {
		if _, err := db.Exec(`INSERT INTO logs(timestamp,severity_number,body,service_name,date) VALUES (?,9,?,'app','2025-01-02')`, at, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) storage.PageResult {
		t.Helper()
		var out bytes.Buffer
		cmd := pageCommand()
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var result storage.PageResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := invoke("logs", "--service", "app", "--since", "2025-01-02T11:00:00Z", "--until", "2025-01-02T13:00:00Z", "--limit", "2")
	if first.NextCursor == "" || len(first.Records.([]any)) != 2 {
		t.Fatalf("first page: %+v", first)
	}
	second := invoke("logs", "--cursor", first.NextCursor)
	if second.NextCursor != "" || len(second.Records.([]any)) != 1 {
		t.Fatalf("second page: %+v", second)
	}
	if first.Signal != "logs" || second.Signal != "logs" || !first.Since.Equal(second.Since) || !first.Until.Equal(second.Until) {
		t.Fatalf("window changed: %+v %+v", first, second)
	}
}

func TestPageCommandRejectsInvalidArgumentsBeforeDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, args := range [][]string{
		{"traces", "--since", "1h", "--limit", "0"},
		{"logs", "--since", "1h", "--limit", "1001"},
		{"logs", "--since", "1h", "--limit", "1", "--service="},
		{"logs", "--cursor", "abc", "--limit", "1"},
		{"logs", "--cursor="},
		{"logs", "--cursor", strings.Repeat("x", 16385)},
	} {
		cmd := pageCommand()
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected failure: %q", args)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("invalid args opened database: %v", err)
	}
}
