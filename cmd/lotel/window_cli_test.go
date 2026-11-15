package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedCommandsRejectUnencodableUTCWindow(t *testing.T) {
	commands := [][]string{
		{"query", "recent", "traces", "--limit", "1"},
		{"query", "recent", "logs", "--limit", "1"},
		{"assert", "signal", "--signal", "traces", "--service", "svc"},
		{"query", "span-stats"},
		{"query", "log-stats"},
	}
	for _, command := range commands {
		t.Run(strings.Join(command[:2], "-"), func(t *testing.T) {
			for _, bounds := range [][]string{
				{"--since", "0000-01-01T00:00:00+01:00"},
				{"--since", "0000-01-01T00:00:00Z", "--until", "9999-12-31T23:59:59-01:00"},
			} {
				home := t.TempDir()
				args := append([]string{"-test.run=^TestCLIHelper$", "--"}, command...)
				args = append(args, bounds...)
				cmd := exec.Command(os.Args[0], args...)
				cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
				output, err := cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "UTC year") {
					t.Fatalf("accepted unencodable UTC window or gave wrong error: %s", output)
				}
				if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
					t.Fatalf("invalid window opened database: %v; output: %s", err, output)
				}
			}
		})
	}
}

func TestBoundedCommandsAcceptYearZeroUTC(t *testing.T) {
	commands := [][]string{
		{"query", "recent", "traces", "--limit", "1"},
		{"query", "recent", "logs", "--limit", "1"},
		{"assert", "signal", "--signal", "traces", "--service", "svc"},
		{"query", "span-stats"},
		{"query", "log-stats"},
	}
	for _, command := range commands {
		t.Run(strings.Join(command[:2], "-"), func(t *testing.T) {
			home := t.TempDir()
			args := append([]string{"-test.run=^TestCLIHelper$", "--"}, command...)
			args = append(args, "--since", "0000-01-01T00:00:00Z")
			cmd := exec.Command(os.Args[0], args...)
			cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
			output, err := cmd.CombinedOutput()
			if command[0] != "assert" && err != nil {
				t.Fatalf("valid UTC year-zero bound rejected: %s: %v", output, err)
			}
			if command[0] == "assert" && (err == nil || !strings.Contains(string(output), `"passed":false`)) {
				t.Fatalf("expected missing evidence for valid bound: %s: %v", output, err)
			}
			if !json.Valid(output) {
				t.Fatalf("invalid JSON: %s", output)
			}
		})
	}
}
