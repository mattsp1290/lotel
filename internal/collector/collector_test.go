package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartEnablesProfiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PATH", tmp)
	argsPath := filepath.Join(tmp, "docker-args")
	t.Setenv("LOTEL_TEST_DOCKER_ARGS", argsPath)
	script := "#!/bin/sh\ncase \"$1\" in\ninspect) exit 1;;\nrun) printf '%s\\n' \"$@\" > \"$LOTEL_TEST_DOCKER_ARGS\"; printf 'test-container\\n';;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(tmp, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfg, []byte("service: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(tmp, "data")
	if err := Start(context.Background(), cfg, dataPath, "docker"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	command := defaultImage + "\n--config=/etc/otelcol-contrib/config.yaml\n--feature-gates=+service.profilesSupport\n"
	if !strings.HasSuffix(string(args), command) {
		t.Fatalf("collector command does not enable profiles with its config: %s", args)
	}
	if _, err := os.Stat(filepath.Join(dataPath, "profiles")); err != nil {
		t.Fatal(err)
	}
	state, err := readState()
	if err != nil || state == nil || !state.ProfilesEnabled {
		t.Fatalf("profile capability was not persisted: %+v %v", state, err)
	}
}

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
		ContainerID:     "abc123def456",
		ContainerName:   "lotel-collector",
		Image:           "otel/opentelemetry-collector-contrib:latest",
		StartedAt:       time.Now(),
		ConfigPath:      "/tmp/config.yaml",
		DataPath:        "/tmp/data",
		ProfilesEnabled: true,
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
	if !got.ProfilesEnabled {
		t.Error("profiles launch capability was not persisted")
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
	// An unavailable Docker executable must be reported as not running.
	t.Setenv("PATH", t.TempDir())
	running := isContainerRunning()
	if running {
		t.Error("expected false when no lotel-collector container exists")
	}
}

func TestStatusPreservesLifecycleState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	state := &State{ContainerName: "lotel-collector", ContainerID: "previous-container"}
	if err := writeState(state); err != nil {
		t.Fatal(err)
	}
	status, err := GetStatus(t.Context())
	if err != nil || status.Running {
		t.Fatalf("status: %+v, %v", status, err)
	}
	saved, err := readState()
	if err != nil || saved == nil || saved.ContainerID != state.ContainerID {
		t.Fatalf("status changed lifecycle state: %+v, %v", saved, err)
	}
}

func TestCheckHealth_NoServer(t *testing.T) {
	// Exercise an unreachable endpoint independently of the local collector.
	old := healthURL
	healthURL = "http://127.0.0.1:0/"
	defer func() { healthURL = old }()
	healthy := checkHealth()
	if healthy {
		t.Error("expected unhealthy when no server is running")
	}
}
