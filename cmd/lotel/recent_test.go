package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestRecentCLI(t *testing.T) {
	build := func(names []string) string {
		home := t.TempDir()
		path := filepath.Join(home, ".lotel", "data")
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		db, err := storage.OpenDB(filepath.Join(path, "lotel.db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			timestamp := "2025-01-01T00:00:01Z"
			if name == "older" {
				timestamp = "2025-01-01T00:00:00Z"
			}
			_, err = db.Exec(`INSERT INTO traces (trace_id, span_id, name, kind, start_time, end_time, duration_ns, status_code, service_name, attributes, date) VALUES ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','1111111111111111',?,1,?,?,0,2,'app',?, '2025-01-01')`, name, timestamp, timestamp, `{"value":"`+name+`"}`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`INSERT INTO logs (timestamp, severity_number, body, service_name, trace_id, span_id, attributes, date) VALUES (?,17,?,'app','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','1111111111111111',?, '2025-01-01')`, timestamp, name, `{"value":"`+name+`"}`)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		return home
	}
	home := build([]string{"b", "a", "a", "older"})
	reversed := build([]string{"older", "a", "a", "b"})
	invoke := func(home, signal string, extra ...string) ([]byte, error) {
		args := []string{"-test.run=^TestCLIHelper$", "--", "query", "recent", signal, "--since", "2025-01-01T00:00:00Z", "--until", "2025-01-01T00:00:02Z"}
		args = append(args, extra...)
		cmd := exec.Command(os.Args[0], args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
		return cmd.CombinedOutput()
	}
	for _, signal := range []string{"traces", "logs"} {
		t.Run(signal, func(t *testing.T) {
			out, err := invoke(home, signal, "--limit", "2")
			if err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			var result struct {
				Records   []map[string]any `json:"records"`
				Truncated bool             `json:"truncated"`
			}
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			if !result.Truncated || len(result.Records) != 2 {
				t.Fatalf("unexpected result: %s", out)
			}
			repeat, err := invoke(reversed, signal, "--limit", "2")
			if err != nil || string(out) != string(repeat) {
				t.Fatalf("unstable: %s %s %v", out, repeat, err)
			}
			out, err = invoke(home, signal, "--limit", "3")
			if err != nil || json.Unmarshal(out, &result) != nil || !result.Truncated || len(result.Records) != 3 {
				t.Fatalf("unexpected lookahead result: %s %v", out, err)
			}
			out, err = invoke(home, signal, "--limit", "4")
			if err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			if err := json.Unmarshal(out, &result); err != nil || result.Truncated || len(result.Records) != 4 {
				t.Fatalf("unexpected full result: %s %v", out, err)
			}
			out, err = invoke(home, signal, "--limit", "5")
			if err != nil || json.Unmarshal(out, &result) != nil || result.Truncated || len(result.Records) != 4 {
				t.Fatalf("unexpected larger limit: %s %v", out, err)
			}
			out, err = invoke(home, signal, "--limit", "5", "--since", "2025-01-01T00:00:01.000000001Z")
			if err != nil || json.Unmarshal(out, &result) != nil || len(result.Records) != 0 {
				t.Fatalf("included records outside precise window: %s %v", out, err)
			}
			filter := []string{"--limit", "1", "--name", "older"}
			if signal == "logs" {
				filter = []string{"--limit", "1", "--body-contains", "older"}
			}
			out, err = invoke(home, signal, filter...)
			if err != nil || !strings.Contains(string(out), "older") || !strings.Contains(string(out), `"truncated":false`) {
				t.Fatalf("filter after limit: %s %v", out, err)
			}
			out, err = invoke(home, signal, "--limit", "1", "--service", "missing")
			if err != nil || !strings.Contains(string(out), `"records":[]`) || !strings.Contains(string(out), `"truncated":false`) {
				t.Fatalf("unexpected empty result: %s %v", out, err)
			}
			if _, err := invoke(home, signal, "--limit", "0"); err == nil {
				t.Fatal("zero limit accepted")
			}
			if _, err := invoke(home, signal, "--limit", "1001"); err == nil {
				t.Fatal("oversize limit accepted")
			}
			unused := t.TempDir()
			if _, err := invoke(unused, signal, "--limit", "1", "--until", ""); err == nil {
				t.Fatal("empty end accepted")
			}
			if _, err := os.Stat(filepath.Join(unused, ".lotel")); !os.IsNotExist(err) {
				t.Fatalf("invalid request opened database: %v", err)
			}
		})
	}
}
