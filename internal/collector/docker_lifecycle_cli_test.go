//go:build !windows

package collector_test

import (
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

type dockerLifecycleFixture struct {
	nativeCLIFixture
	root, statePath string
	original        []byte
}

func newDockerLifecycleFixture(t *testing.T) dockerLifecycleFixture {
	t.Helper()
	f := newNativeCLIFixture(t, "")
	root := t.TempDir()
	docker := `#!/bin/sh
printf '%s\n' "$*" >> "$LOTEL_LIFECYCLE_ROOT/calls"
case "$1" in
inspect)
 [ ! -f "$LOTEL_LIFECYCLE_ROOT/unavailable" ] || exit 125
 if [ "$2" = "-f" ]; then
  n=0
  if [ -f "$LOTEL_LIFECYCLE_ROOT/count" ]; then read n < "$LOTEL_LIFECYCLE_ROOT/count"; fi
  n=$((n+1)); printf '%s\n' "$n" > "$LOTEL_LIFECYCLE_ROOT/count"
  if [ "$n" = "$LOTEL_LIFECYCLE_FAIL_PROBE" ]; then exit 125; fi
  [ -f "$LOTEL_LIFECYCLE_ROOT/running" ] || exit 1
  printf true
 else
  [ -f "$LOTEL_LIFECYCLE_ROOT/running" ]
 fi;;
ps)
 [ ! -f "$LOTEL_LIFECYCLE_ROOT/unavailable" ] || exit 125
 if [ -f "$LOTEL_LIFECYCLE_ROOT/running" ]; then printf '0123456789ab\n'; fi;;
stop)
 [ ! -f "$LOTEL_LIFECYCLE_ROOT/stop-fails" ] || exit 125
 /bin/rm -f "$LOTEL_LIFECYCLE_ROOT/running";;
rm)
 [ ! -f "$LOTEL_LIFECYCLE_ROOT/stop-fails" ] || exit 125
 /bin/rm -f "$LOTEL_LIFECYCLE_ROOT/running";;
run)
 printf running > "$LOTEL_LIFECYCLE_ROOT/running"
 if [ "$LOTEL_LIFECYCLE_ROLLBACK_FAIL" = 1 ]; then printf fail > "$LOTEL_LIFECYCLE_ROOT/stop-fails"; fi
 printf new-container;;
*) exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	f.env = append(f.env, "PATH="+root, "LOTEL_LIFECYCLE_ROOT="+root)
	configPath := filepath.Join(f.dir, "lotel-collector.yaml")
	source, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	state := collector.State{ContainerID: "existing-container", ContainerName: "lotel-collector", Image: "otel/opentelemetry-collector-contrib:0.165.0", StartedAt: time.Now(), ConfigPath: configPath, ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256(source)), DataPath: filepath.Join(f.home, ".lotel/data"), ProfilesEnabled: true}
	original, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.home, ".lotel/collector.state")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "running"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(root, "unavailable"))
		_ = os.Remove(filepath.Join(root, "stop-fails"))
		if out, err := f.run("stop"); err != nil {
			t.Errorf("Docker cleanup: %v %s", err, out)
		}
	})
	return dockerLifecycleFixture{f, root, path, original}
}

func (f dockerLifecycleFixture) assertRetained(t *testing.T, original []byte) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(f.root, "running")); err != nil {
		t.Fatalf("existing collection lost: %v", err)
	}
	current, err := os.ReadFile(f.statePath)
	if err != nil || string(current) != string(original) {
		t.Fatalf("existing state changed: %s %v", current, err)
	}
}

func TestDockerStartupPreservesExistingCollection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		probe  int
		native bool
	}{
		{"adopted-child-failure", 0, false},
		{"first-inspection-fails", 1, false},
		{"former-second-inspection-eliminated", 2, false},
		{"native-backend-unknown-guard", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDockerLifecycleFixture(t)
			longTemp := filepath.Join(t.TempDir(), strings.Repeat("socket-path-", 12))
			if err := os.MkdirAll(longTemp, 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := f.command(t.Context(), "start")
			if tc.native {
				cmd = f.command(t.Context(), "start", "--backend", "native")
			}
			cmd.Env = append(f.env, "TMPDIR="+longTemp, fmt.Sprintf("LOTEL_LIFECYCLE_FAIL_PROBE=%d", tc.probe))
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("startup unexpectedly succeeded: %s", out)
			}
			f.assertRetained(t, f.original)
			calls, err := os.ReadFile(filepath.Join(f.root, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(calls), "\n") {
				if strings.HasPrefix(line, "stop ") || strings.HasPrefix(line, "rm ") || strings.HasPrefix(line, "run ") {
					t.Fatalf("startup mutated unknown/adopted collection: %s", calls)
				}
			}
			if tc.probe == 1 && !strings.Contains(string(out), "inspect Collector running state") {
				t.Fatalf("missing liveness error: %s", out)
			}
		})
	}
}

func TestDockerStartupRollsBackOnlyNewContainer(t *testing.T) {
	f := newDockerLifecycleFixture(t)
	if err := os.Remove(filepath.Join(f.root, "running")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.statePath); err != nil {
		t.Fatal(err)
	}
	longTemp := filepath.Join(t.TempDir(), strings.Repeat("socket-path-", 12))
	if err := os.MkdirAll(longTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := f.command(t.Context(), "start")
	cmd.Env = append(f.env, "TMPDIR="+longTemp)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("invalid socket unexpectedly succeeded: %s", out)
	}
	if _, err := os.Stat(filepath.Join(f.root, "running")); !os.IsNotExist(err) {
		t.Fatalf("new container left running: %v", err)
	}
	if _, err := os.Stat(f.statePath); !os.IsNotExist(err) {
		t.Fatalf("new state left behind: %v", err)
	}
	calls, err := os.ReadFile(filepath.Join(f.root, "calls"))
	if err != nil || !strings.Contains(string(calls), "run -d") || !strings.Contains(string(calls), "stop lotel-collector") || !strings.Contains(string(calls), "rm lotel-collector") {
		t.Fatalf("new-container rollback not exercised: %s %v", calls, err)
	}
}

func TestDockerStopRetainsUnconfirmedCollection(t *testing.T) {
	for _, managed := range []bool{false, true} {
		for _, fault := range []string{"unavailable", "stop-fails"} {
			t.Run(fmt.Sprintf("managed=%t/%s", managed, fault), func(t *testing.T) {
				f := newDockerLifecycleFixture(t)
				if managed {
					if out, err := f.run("start"); err != nil {
						t.Fatalf("adopt: %v %s", err, out)
					}
				}
				original, err := os.ReadFile(f.statePath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.root, fault), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if out, err := f.run("stop"); err == nil {
					t.Fatalf("unconfirmed stop succeeded: %s", out)
				}
				f.assertRetained(t, original)
				if err := os.Remove(filepath.Join(f.root, fault)); err != nil {
					t.Fatal(err)
				}
				if out, err := f.run("stop"); err != nil {
					t.Fatalf("recovered stop: %v %s", err, out)
				}
				if _, err := os.Stat(f.statePath); !os.IsNotExist(err) {
					t.Fatalf("recovered stop retained state: %v", err)
				}
			})
		}
	}
}

func TestDockerStopClearsStateAfterFinalIndexingError(t *testing.T) {
	f := newDockerLifecycleFixture(t)
	if out, err := f.run("start"); err != nil {
		t.Fatalf("adopt: %v %s", err, out)
	}
	release := holdNativeDatabase(t, filepath.Join(f.home, ".lotel/data/lotel.db"))
	if out, err := f.run("stop"); err == nil || !strings.Contains(string(out), "final ingestion") {
		t.Fatalf("final indexing error lost: %v %s", err, out)
	}
	release()
	if _, err := os.Stat(filepath.Join(f.root, "running")); !os.IsNotExist(err) {
		t.Fatalf("collection still running: %v", err)
	}
	if _, err := os.Stat(f.statePath); !os.IsNotExist(err) {
		t.Fatalf("confirmed stopped collection retained state: %v", err)
	}
}

func TestDockerStartupRetainsStateWhenNewContainerRollbackFails(t *testing.T) {
	f := newDockerLifecycleFixture(t)
	if err := os.Remove(filepath.Join(f.root, "running")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.statePath); err != nil {
		t.Fatal(err)
	}
	longTemp := filepath.Join(t.TempDir(), strings.Repeat("socket-path-", 12))
	if err := os.MkdirAll(longTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := f.command(t.Context(), "start")
	cmd.Env = append(f.env, "TMPDIR="+longTemp, "LOTEL_LIFECYCLE_ROLLBACK_FAIL=1")
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "rolling back newly started Docker Collector; state retained") {
		t.Fatalf("rollback failure was hidden: %v %s", err, out)
	}
	current, err := os.ReadFile(f.statePath)
	var state collector.State
	if err != nil || json.Unmarshal(current, &state) != nil || state.ContainerID != "new-container" {
		t.Fatalf("failed rollback lost new container ownership: %s %v", current, err)
	}
	f.assertRetained(t, current)
}

func TestNativeStartupWithoutRecordedDockerOwnership(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprintf("known-running=%t", running), func(t *testing.T) {
			f := newNativeCLIFixture(t, "")
			bin := t.TempDir()
			script := "#!/bin/sh\nexit 125\n"
			if running {
				script = "#!/bin/sh\nprintf true\n"
			}
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			f.env = append(f.env, "PATH="+bin)
			out, err := f.run("start", "--backend", "native", "--wait")
			if running {
				if err == nil || !strings.Contains(string(out), "Docker collector is running") {
					t.Fatalf("confirmed Docker collection was not guarded: %v %s", err, out)
				}
			} else {
				if err != nil {
					t.Fatalf("Docker API unavailability blocked native mode without recorded Docker ownership: %v %s", err, out)
				}
				out, err = f.run("status")
				var status collector.Status
				if err != nil || json.Unmarshal(out, &status) != nil || !status.Running || status.Backend != "native" {
					t.Fatalf("native mode was not running: %v %s", err, out)
				}
			}
		})
	}
}

func TestNativeStartupPreservesUnknownRecordedDockerOwnership(t *testing.T) {
	f := newDockerLifecycleFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "unavailable"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start", "--backend", "native"); err == nil || !strings.Contains(string(out), "inspect Collector running state") {
		t.Fatalf("native startup ignored recorded Docker ownership: %v %s", err, out)
	}
	f.assertRetained(t, f.original)
	calls, err := os.ReadFile(filepath.Join(f.root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(calls), "\n") {
		if strings.HasPrefix(line, "stop ") || strings.HasPrefix(line, "rm ") || strings.HasPrefix(line, "run ") {
			t.Fatalf("unknown recorded Docker ownership caused mutation: %s", calls)
		}
	}
}
