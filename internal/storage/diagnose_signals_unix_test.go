//go:build !windows

package storage

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDiagnoseSignalsRejectsSymlinkAndFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lotel.db")
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path+".lock"); err != nil {
		t.Fatal(err)
	}
	if got := DiagnoseSignals(context.Background(), path, "app", start, end); got.Observation != "unavailable" {
		t.Fatalf("symlink: %+v", got)
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path+".lock", 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan SignalDiagnosis, 1)
	go func() { done <- DiagnoseSignals(context.Background(), path, "app", start, end) }()
	select {
	case got := <-done:
		if got.Observation != "unavailable" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked")
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if got := DiagnoseSignals(context.Background(), path, "app", start, end); got.Observation != "unavailable" {
		t.Fatalf("database symlink: %+v", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if got := DiagnoseSignals(context.Background(), path, "app", start, end); got.Observation != "unavailable" {
		t.Fatalf("database FIFO: %+v", got)
	}
}
