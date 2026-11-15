package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestLogStatsCLI(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".lotel", "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(filepath.Join(data, "lotel.db"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	_, err = db.Exec(`INSERT INTO logs (timestamp, severity_number, body, service_name, date) VALUES (?, 9, '100% _done', 'app', CURRENT_DATE), (?, 17, '100% _done', 'app', CURRENT_DATE), (?, 21, 'other', 'app', CURRENT_DATE), (?, 17, '100% _done', 'other', CURRENT_DATE)`, at, at, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	invoke := func(home string, args ...string) (string, string, error) {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--", "query", "log-stats"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	args := []string{"--since", at.Format(time.RFC3339), "--until", at.Format(time.RFC3339), "--service", "app", "--min-severity", "error", "--body-contains", "100% _DONE", "--limit", "1"}
	out, stderr, err := invoke(home, args...)
	if err != nil {
		t.Fatalf("query: %s %v", stderr, err)
	}
	var rows []storage.LogStats
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Severity != "error" || rows[0].LogCount != 1 {
		t.Fatalf("result: %s", out)
	}
	out, stderr, err = invoke(home, "--since", at.Add(time.Nanosecond).Format(time.RFC3339Nano), "--until", at.Add(time.Second).Format(time.RFC3339))
	if err != nil || out != "[]\n" {
		t.Fatalf("fractional window: %q %q %v", out, stderr, err)
	}
	for _, args := range [][]string{
		{}, {"--since", "0s"}, {"--since", "-1h"}, {"--since", "1hX"}, {"--since", "1000000000000000000d"},
		{"--since", "2025-01-02T12:00:00.0000000001Z"}, {"--since", "1h", "--until", ""},
		{"--since", "1h", "--until", "9999-01-01T00:00:00Z"}, {"--since", "2025-01-03T00:00:00Z", "--until", "2025-01-02T00:00:00Z"},
		{"--since", "1h", "--limit", "-1"}, {"--since", "1h", "--min-severity", "bad"}, {"--since", "1h", "--body-contains", ""},
	} {
		unused := t.TempDir()
		_, _, err := invoke(unused, args...)
		if err == nil {
			t.Errorf("accepted %q", args)
		}
		if _, err := os.Stat(filepath.Join(unused, ".lotel")); !os.IsNotExist(err) {
			t.Errorf("opened database for %q: %v", args, err)
		}
	}
}
