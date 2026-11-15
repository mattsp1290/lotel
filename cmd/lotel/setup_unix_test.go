//go:build darwin || linux

package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestSetupSkipsFIFO(t *testing.T) {
	project := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(project, "go.mod"), 0600); err != nil {
		t.Fatal(err)
	}
	report, _, err := runSetup(t, "--recommend-only", "--path", project)
	if err != nil || len(report.Candidates) != 0 || len(report.Skipped) != 1 || report.Skipped[0] != (setupSkipped{Manifest: "go.mod", Reason: "not_regular"}) {
		t.Fatalf("FIFO report: %+v %v", report, err)
	}
}
