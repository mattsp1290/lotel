package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundtrip(t *testing.T) {
	// Use a temp directory for state.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// No state should exist initially.
	s, err := readState()
	if err != nil {
		t.Fatalf("readState: %v", err)
	}
	if s != nil {
		t.Fatal("expected nil state initially")
	}

	// Write state.
	state := &State{
		PID:    12345,
		Binary: "/usr/bin/otelcol-contrib",
	}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	// Read it back.
	got, err := readState()
	if err != nil {
		t.Fatalf("readState: %v", err)
	}
	if got.PID != 12345 {
		t.Errorf("PID = %d, want 12345", got.PID)
	}
	if got.Binary != "/usr/bin/otelcol-contrib" {
		t.Errorf("Binary = %q, want /usr/bin/otelcol-contrib", got.Binary)
	}

	// Remove state.
	if err := removeState(); err != nil {
		t.Fatalf("removeState: %v", err)
	}
	s, err = readState()
	if err != nil {
		t.Fatalf("readState after remove: %v", err)
	}
	if s != nil {
		t.Fatal("expected nil state after remove")
	}
}

func TestResolveConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Create the .lotel directory that resolveConfig writes to.
	if err := os.MkdirAll(filepath.Join(tmp, ".lotel"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a source config with /data/ references.
	srcDir := filepath.Join(tmp, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcConfig := filepath.Join(srcDir, "config.yaml")
	if err := os.WriteFile(srcConfig, []byte("path: /data/traces/traces.jsonl\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dataPath := filepath.Join(tmp, "mydata")
	resolved, err := resolveConfig(srcConfig, dataPath)
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("reading resolved config: %v", err)
	}

	expected := "path: " + dataPath + "/traces/traces.jsonl\n"
	if string(content) != expected {
		t.Errorf("resolved config = %q, want %q", string(content), expected)
	}
}

func TestIsProcessAlive_DeadPID(t *testing.T) {
	// PID 0 should not be alive.
	alive := isProcessAlive(&State{PID: 0})
	if alive {
		t.Error("PID 0 should not be alive")
	}

	// Nil state should not be alive.
	alive = isProcessAlive(nil)
	if alive {
		t.Error("nil state should not be alive")
	}

	// A very high PID that almost certainly doesn't exist.
	alive = isProcessAlive(&State{PID: 999999999})
	if alive {
		t.Error("PID 999999999 should not be alive")
	}
}

func TestCheckHealth_NoServer(t *testing.T) {
	// No server running on 13133, should return false.
	healthy := checkHealth()
	if healthy {
		t.Error("expected unhealthy when no server is running")
	}
}
