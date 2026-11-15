package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupWindowsCurrentDriveRoot(t *testing.T) {
	report, _, err := runSetup(t, "--recommend-only", "--path", `\`)
	if err != nil || report.Path != `\` {
		t.Fatalf("current drive root: %+v %v", report, err)
	}
}

func TestSetupWindowsVerbatimPath(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(project, `\\`) {
		t.Skip("test needs a drive-letter temporary directory")
	}
	verbatim := `\\?\` + project
	report, _, err := runSetup(t, "--recommend-only", "--path", verbatim)
	if err != nil || report.Path != verbatim || len(report.Candidates) != 1 || report.Candidates[0].Ecosystem != "Go" {
		t.Fatalf("verbatim directory: %+v %v", report, err)
	}
}
