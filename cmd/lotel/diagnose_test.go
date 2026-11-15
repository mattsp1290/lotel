package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingDiagnosisWriter struct{}

func (failingDiagnosisWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestDiagnoseRuntimeCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := diagnoseCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"runtime"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"observation": "missing"`) {
		t.Fatalf("missing state not reported: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("diagnosis created files: %v", err)
	}
	cmd = diagnoseCommand()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetOut(failingDiagnosisWriter{})
	cmd.SetArgs([]string{"runtime"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("output error was ignored")
	}
	cmd = diagnoseCommand()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"runtime", "extra"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("unexpected argument accepted")
	}
}
