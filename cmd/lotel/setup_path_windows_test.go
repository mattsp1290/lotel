package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSetupWindowsUnsupportedWithoutConnecting(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\lotel-setup-test-%d`, os.Getpid())
	name16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := windows.CreateNamedPipe(name16, windows.PIPE_ACCESS_DUPLEX, windows.PIPE_TYPE_BYTE|windows.PIPE_NOWAIT, 1, 256, 256, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pipe)
	_, output, err := runSetup(t, "--recommend-only", "--path", name)
	if err == nil || !strings.Contains(err.Error(), "not supported on Windows") || len(output) != 0 {
		t.Fatalf("expected unsupported error without output: %q %v", output, err)
	}
	if err := windows.ConnectNamedPipe(pipe, nil); !errors.Is(err, windows.ERROR_PIPE_LISTENING) {
		t.Fatalf("pipe connected or unavailable: %v", err)
	}
}

func TestSetupWindowsUnsupportedWithoutChanges(t *testing.T) {
	dir, err := os.MkdirTemp("", "lotel-setup-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(dir)
	manifest := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(manifest, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(manifest)
	_, output, err := runSetup(t, "--recommend-only", "--path", dir)
	if err == nil || !strings.Contains(err.Error(), "not supported on Windows") || len(output) != 0 {
		t.Fatalf("expected unsupported error without output: %q %v", output, err)
	}
	content, err := os.ReadFile(manifest)
	if err != nil || string(content) != "unchanged" {
		t.Fatalf("manifest changed: %q %v", content, err)
	}
}
