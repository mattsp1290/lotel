package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectForUpgradeIsReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, ids, running                              string
		listError, inspectError, wantRunning, wantError bool
	}{
		{"absent", "", "", false, false, false, false},
		{"stopped", "0123456789ab", "false", false, false, false, false},
		{"running", "0123456789ab", "true", false, false, true, false},
		{"daemon unavailable", "", "", true, false, false, true},
		{"inspect failed", "0123456789ab", "true", false, true, false, true},
		{"bad id", "unexpected", "true", false, false, false, true},
		{"multiple containers", "0123456789ab 123456789abc", "true", false, false, false, true},
		{"bad running state", "0123456789ab", "unknown", false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", bin)
			t.Setenv("TEST_IDS", tc.ids)
			t.Setenv("TEST_RUNNING", tc.running)
			if tc.listError {
				t.Setenv("TEST_LIST_ERROR", "1")
			}
			if tc.inspectError {
				t.Setenv("TEST_INSPECT_ERROR", "1")
			}
			program := `#!/bin/sh
case "$1" in
ps) [ -z "$TEST_LIST_ERROR" ] || exit 125; printf '%s' "$TEST_IDS";;
inspect) [ -z "$TEST_INSPECT_ERROR" ] || exit 125; printf '%s' "$TEST_RUNNING";;
*) exit 1;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(program), 0o755); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(home, stateDir, stateFile)
			if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
				t.Fatal(err)
			}
			original := []byte(`{"config_path":"/custom/config.yaml"}`)
			if err := os.WriteFile(statePath, original, 0o600); err != nil {
				t.Fatal(err)
			}
			status, err := InspectForUpgrade(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			if err == nil && status.Running != tc.wantRunning {
				t.Fatalf("status=%+v", status)
			}
			if tc.wantRunning && status.ConfigPath != "/custom/config.yaml" {
				t.Fatalf("status=%+v", status)
			}
			got, err := os.ReadFile(statePath)
			if err != nil || string(got) != string(original) {
				t.Fatalf("state=%q err=%v", got, err)
			}
		})
	}
}

func TestInspectForUpgradeAbsentCreatesNoState(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectForUpgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, stateDir)); !os.IsNotExist(err) {
		t.Fatalf("created state directory: %v", err)
	}
}

func TestInspectForUpgradeWithoutDocker(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		t.Run(fmt.Sprintf("recorded=%t", recorded), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", t.TempDir())
			if recorded {
				if err := writeState(&State{Backend: "docker", ContainerID: "recorded-container"}); err != nil {
					t.Fatal(err)
				}
			}
			status, err := InspectForUpgrade(t.Context())
			if recorded {
				if err == nil {
					t.Fatalf("ignored uninspectable recorded Docker state: %+v", status)
				}
			} else if err != nil || status.Running {
				t.Fatalf("absent Collector requires Docker: %+v %v", status, err)
			}
			if !recorded {
				if _, err := os.Stat(filepath.Join(home, stateDir)); !os.IsNotExist(err) {
					t.Fatalf("created state directory: %v", err)
				}
			}
		})
	}
}

func TestInspectForUpgradePreservesNativeBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir, err := os.MkdirTemp("", "lotel-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(Status{Backend: "native", Running: true, Healthy: true})
	})}
	go server.Serve(listener)
	defer server.Close()
	state := &State{Backend: "native", ConfigPath: "/custom/native.yaml", ControlSocket: socket, ControlToken: "token"}
	if err := writeState(state); err != nil {
		t.Fatal(err)
	}
	status, err := InspectForUpgrade(context.Background())
	if err != nil || !status.Running || status.Backend != "native" || status.ConfigPath != state.ConfigPath {
		t.Fatalf("upgrade lost native lifecycle: %+v %v", status, err)
	}
}
