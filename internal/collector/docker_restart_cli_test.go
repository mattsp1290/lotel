//go:build !windows

package collector_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
)

func TestDockerConfigRestart(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	binDir := t.TempDir()
	running := filepath.Join(t.TempDir(), "running")
	if err := os.WriteFile(running, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	docker := `#!/bin/sh
case "$1" in
  inspect)
    if [ ! -f "$LOTEL_DOCKER_TEST_RUNNING" ]; then exit 1; fi
    if [ "$2" = "-f" ]; then printf 'true\n'; fi
    ;;
  stop|rm) /bin/rm -f "$LOTEL_DOCKER_TEST_RUNNING" ;;
  run)
    printf ready > "$LOTEL_DOCKER_TEST_RUNNING"
    printf 'replacement-container\n'
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(f.home, ".lotel", "collector.state")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath, err := filepath.EvalSymlinks(filepath.Join(f.dir, "lotel-collector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	state := collector.State{ContainerID: "original-container", Image: "otel/opentelemetry-collector-contrib:0.165.0", ConfigPath: configPath, ConfigSHA256: "old-config", DataPath: filepath.Join(f.home, ".lotel", "data")}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := f.command(ctx, "start")
	cmd.Env = append(f.env, "PATH="+binDir, "LOTEL_DOCKER_TEST_RUNNING="+running)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Docker config restart did not complete: %v\n%s", err, out)
	}
	encoded, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &state); err != nil {
		t.Fatal(err)
	}
	if state.ContainerID != "replacement-container" || state.ConfigSHA256 == "old-config" {
		t.Fatalf("Docker config restart did not replace persisted state: %+v", state)
	}
}
