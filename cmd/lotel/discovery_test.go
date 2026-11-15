package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryRejectsInvalidBoundsBeforeOpeningDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"services", "operations"} {
		for _, args := range [][]string{
			{"--limit=-1"},
			{"--since=2026-01-02T00:00:00Z", "--until=2026-01-01T00:00:00Z"},
		} {
			cmd := discoveryCommand(name)
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil {
				t.Errorf("%s accepted %v", name, args)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("invalid bounds opened database: %v", err)
	}
}
