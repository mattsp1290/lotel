package collector

import (
	"context"
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
