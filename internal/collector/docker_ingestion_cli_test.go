//go:build !windows

package collector_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
)

func TestDockerAdoptionRunsAutomaticIngestion(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	configPath := filepath.Join(f.dir, "lotel-collector.yaml")
	source, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	source = []byte(strings.ReplaceAll(string(source), "interval: 1h", "interval: 25ms"))
	if err := os.WriteFile(configPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	running := filepath.Join(t.TempDir(), "running")
	allowRun := filepath.Join(t.TempDir(), "allow-run")
	probe := filepath.Join(t.TempDir(), "probe")
	calls := filepath.Join(t.TempDir(), "calls")
	if err := os.WriteFile(running, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	docker := `#!/bin/sh
case "$1" in
inspect)
  [ ! -f "$LOTEL_DOCKER_TEST_PROBE.unavailable" ] || exit 125
  if [ -f "$LOTEL_DOCKER_TEST_PROBE.fail" ]; then
    /bin/rm "$LOTEL_DOCKER_TEST_PROBE.fail"
    printf failed > "$LOTEL_DOCKER_TEST_PROBE.failed"
    printf 'temporary Docker API error\n' >&2
    exit 125
  fi
  [ ! -f "$LOTEL_DOCKER_TEST_PROBE.failed" ] || printf recovered > "$LOTEL_DOCKER_TEST_PROBE.recovered"
  [ -f "$LOTEL_DOCKER_TEST_RUNNING" ] || exit 1
  if [ "$2" = "-f" ]; then printf 'true\n'; fi;;
ps) [ ! -f "$LOTEL_DOCKER_TEST_PROBE.unavailable" ] || exit 125; if [ -f "$LOTEL_DOCKER_TEST_RUNNING" ]; then printf '0123456789ab\n'; fi;;
stop|rm) printf '%s\n' "$1" >> "$LOTEL_DOCKER_TEST_CALLS"; /bin/rm -f "$LOTEL_DOCKER_TEST_RUNNING";;
run) [ -f "$LOTEL_DOCKER_TEST_ALLOW_RUN" ] || exit 99;printf running > "$LOTEL_DOCKER_TEST_RUNNING";printf 'replacement-container\n';;
*) exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	f.env = append(f.env, "PATH="+binDir, "LOTEL_DOCKER_TEST_RUNNING="+running, "LOTEL_DOCKER_TEST_ALLOW_RUN="+allowRun, "LOTEL_DOCKER_TEST_PROBE="+probe, "LOTEL_DOCKER_TEST_CALLS="+calls)
	dataPath := filepath.Join(f.home, ".lotel", "data")
	statePath := filepath.Join(f.home, ".lotel", "collector.state")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	state := collector.State{ContainerID: "adopted-container", ContainerName: "lotel-collector", Image: "otel/opentelemetry-collector-contrib:0.165.0", StartedAt: time.Now(), ConfigPath: configPath, ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256(source)), DataPath: dataPath, ProfilesEnabled: true}
	encoded, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start"); err != nil {
		t.Fatalf("adopt: %v\n%s", err, out)
	}
	if out, err := f.run("status"); err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	} else {
		if err := json.Unmarshal(out, &state); err != nil {
			t.Fatal(err)
		}
	}
	pid := state.PID
	if pid == 0 {
		t.Fatal("adoption did not start background runtime")
	}
	if out, err := f.run("start"); err != nil {
		t.Fatalf("repeat start: %v\n%s", err, out)
	}
	encoded, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &state); err != nil {
		t.Fatal(err)
	}
	if state.PID != pid || state.ContainerID != "adopted-container" {
		t.Fatalf("repeat start replaced runtime or container: %+v", state)
	}
	logs := filepath.Join(dataPath, "logs", "logs.jsonl")
	payload := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"automatic-docker"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000000","body":{"stringValue":"indexed by adopted runtime"}}]}]}]}` + "\n"
	if err := os.WriteFile(logs, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := f.run("query", "logs", "--service", "automatic-docker")
		var rows []json.RawMessage
		if err == nil && json.Unmarshal(out, &rows) == nil && len(rows) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic query: %v\n%s", err, out)
		}
		time.Sleep(25 * time.Millisecond)
	}
	out, err := f.run("status")
	if err != nil {
		t.Fatal(err)
	}
	var status collector.Status
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatal(err)
	}
	if status.Ingestion.LastSuccess == nil || status.Ingestion.LastError != "" || !status.Ingestion.Running {
		t.Fatalf("ingestion status: %+v", status.Ingestion)
	}
	if err := os.WriteFile(probe+".fail", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(probe + ".recovered"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("monitor did not retry after one failed Docker inspection")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if data, err := os.ReadFile(calls); !os.IsNotExist(err) {
		t.Fatalf("transient inspection caused destructive Docker commands: %s %v", data, err)
	}
	file, err := os.OpenFile(logs, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(strings.ReplaceAll(payload, "indexed by adopted runtime", "indexed after Docker recovered"))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append telemetry: %v %v", writeErr, closeErr)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		out, err := f.run("query", "logs", "--service", "automatic-docker")
		var rows []json.RawMessage
		if err == nil && json.Unmarshal(out, &rows) == nil && len(rows) == 2 && strings.Contains(string(out), "indexed after Docker recovered") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic ingestion did not survive Docker inspection error: %v %s", err, out)
		}
		time.Sleep(25 * time.Millisecond)
	}
	out, err = f.run("status")
	if err != nil || json.Unmarshal(out, &status) != nil || status.PID != pid || !status.Running || !status.Ingestion.Running {
		t.Fatalf("inspection recovery replaced or stopped runtime: %v %s", err, out)
	}
	if err := os.WriteFile(probe+".unavailable", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start"); err == nil {
		t.Fatalf("repeated start accepted an unknown Docker running state: %s", out)
	}
	if err := os.Remove(probe + ".unavailable"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(calls); !os.IsNotExist(err) {
		t.Fatalf("unavailable status caused destructive Docker commands: %s %v", data, err)
	}
	out, err = f.run("status")
	if err != nil || json.Unmarshal(out, &status) != nil || status.PID != pid || !status.Running {
		t.Fatalf("repeated start replaced runtime after unknown status: %v %s", err, out)
	}

	if err := os.WriteFile(allowRun, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(running); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start"); err != nil {
		t.Fatalf("restart externally stopped collector: %v\n%s", err, out)
	}
	encoded, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &state); err != nil {
		t.Fatal(err)
	}
	if state.PID == pid || state.ContainerID != "replacement-container" {
		t.Fatalf("runtime did not replace stopped collection: %+v", state)
	}
	select {
	case <-time.After(1100 * time.Millisecond):
	}
	if _, err := os.Stat(running); err != nil {
		t.Fatalf("old runtime stopped replacement container: %v", err)
	}
	if out, err := f.run("stop"); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}
	if _, err := os.Stat(running); !os.IsNotExist(err) {
		t.Fatalf("stop left collector running: %v", err)
	}

	state.PID = 0
	state.ControlSocket = ""
	state.ControlToken = ""
	original, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(running, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	longTemp := filepath.Join(t.TempDir(), strings.Repeat("socket-path-", 12))
	if err := os.MkdirAll(longTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := f.command(context.Background(), "start")
	cmd.Env = append(f.env, "TMPDIR="+longTemp)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("runtime accepted invalid control socket path: %s", out)
	}
	if _, err := os.Stat(running); err != nil {
		t.Fatalf("failed runtime startup stopped adopted collector: %v", err)
	}
	retained, err := os.ReadFile(statePath)
	if err != nil || string(retained) != string(original) {
		t.Fatalf("failed runtime startup changed adopted state: %s %v", retained, err)
	}
	if out, err := f.run("stop"); err != nil {
		t.Fatalf("legacy stop after failed adoption: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if out, err := f.command(ctx, "status").CombinedOutput(); err == nil {
		t.Fatalf("status running after stop: %s", out)
	}
}
