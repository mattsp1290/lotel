//go:build !windows

package collector

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDiagnoseRuntimeRejectsFIFOAndSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, stateDir)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, stateFile)
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan RuntimeDiagnosis, 1)
	go func() { done <- DiagnoseRuntime(context.Background()) }()
	select {
	case got := <-done:
		if got.StateFile.Observation != "unavailable" || got.Runtime.Observation != "unknown" {
			t.Fatalf("FIFO diagnosis: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("opening a FIFO blocked diagnosis")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "missing"), path); err != nil {
		t.Fatal(err)
	}
	got := DiagnoseRuntime(context.Background())
	if got.StateFile.Observation != "unavailable" || got.Runtime.Observation != "unknown" {
		t.Fatalf("symlink diagnosis: %+v", got)
	}
}
