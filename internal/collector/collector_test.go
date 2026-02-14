package collector

import (
	"testing"
	"time"
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
		ContainerID:   "abc123def456",
		ContainerName: "lotel-collector",
		Image:         "otel/opentelemetry-collector-contrib:latest",
		StartedAt:     time.Now(),
		ConfigPath:    "/tmp/config.yaml",
		DataPath:      "/tmp/data",
	}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	// Read it back.
	got, err := readState()
	if err != nil {
		t.Fatalf("readState: %v", err)
	}
	if got.ContainerID != "abc123def456" {
		t.Errorf("ContainerID = %q, want %q", got.ContainerID, "abc123def456")
	}
	if got.ContainerName != "lotel-collector" {
		t.Errorf("ContainerName = %q, want %q", got.ContainerName, "lotel-collector")
	}
	if got.Image != "otel/opentelemetry-collector-contrib:latest" {
		t.Errorf("Image = %q, want %q", got.Image, "otel/opentelemetry-collector-contrib:latest")
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

func TestIsContainerRunning_NoDocker(t *testing.T) {
	// When no container named lotel-collector exists, should return false.
	running := isContainerRunning()
	if running {
		t.Error("expected false when no lotel-collector container exists")
	}
}

func TestCheckHealth_NoServer(t *testing.T) {
	// No server running on 13133, should return false.
	healthy := checkHealth()
	if healthy {
		t.Error("expected unhealthy when no server is running")
	}
}
