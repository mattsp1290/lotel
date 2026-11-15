//go:build !windows

package collector_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/mattsp1290/lotel/internal/upgrade"
)

func TestStoppedNativeCLIUpgradeWithoutDocker(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	if out, err := f.run("start", "--backend", "native", "--wait"); err != nil {
		t.Fatalf("start native without Docker: %v %s", err, out)
	}
	if out, err := f.run("stop"); err != nil {
		t.Fatalf("stop native: %v %s", err, out)
	}
	statePath := filepath.Join(f.home, ".lotel", "collector.state")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("native stop retained state: %v", err)
	}
	dir := t.TempDir()
	base := (&url.URL{Scheme: "file", Path: dir}).String()
	target := map[string]string{"darwin": "macos", "linux": "linux"}[runtime.GOOS] + "-" + map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	asset := filepath.Join(dir, "download", "v99.0.0", "lotel-"+target)
	if err := os.MkdirAll(filepath.Dir(asset), 0o700); err != nil {
		t.Fatal(err)
	}
	program := []byte("#!/bin/sh\necho 'lotel v99.0.0'\n")
	if err := os.WriteFile(asset, program, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(program)
	manifest := upgrade.Manifest{Schema: 1, Version: "99.0.0", Tag: "v99.0.0", Assets: map[string]string{target: base + "/download/v99.0.0/lotel-" + target}, SHA256: map[string]string{target: hex.EncodeToString(hash[:])}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	latest := filepath.Join(dir, "latest", "download")
	if err := os.MkdirAll(latest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(latest, "lotel-manifest.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	upgrading := f
	upgrading.bin = filepath.Join(t.TempDir(), "lotel")
	original, err := os.ReadFile(f.bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upgrading.bin, original, 0o700); err != nil {
		t.Fatal(err)
	}
	upgrading.env = append(f.env, "LOTEL_RELEASE_BASE_URL="+base)
	out, err := upgrading.run("upgrade", "--force", "--json")
	var report upgrade.Report
	if err != nil || json.Unmarshal(out, &report) != nil || !report.Upgraded || report.CollectorRestarted {
		t.Fatalf("offline upgrade after native stop: %v %s", err, out)
	}
	installed, err := os.ReadFile(upgrading.bin)
	if err != nil || string(installed) != string(program) {
		t.Fatalf("release was not installed: %v", err)
	}
	out, err = f.run("status")
	var status collector.Status
	if err == nil || json.Unmarshal(out, &status) != nil || status.Running {
		t.Fatalf("upgrade restarted stopped collection: %v %s", err, out)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("upgrade created Collector state: %v", err)
	}
}
