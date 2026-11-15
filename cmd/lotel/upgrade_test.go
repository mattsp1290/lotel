package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mattsp1290/lotel/internal/upgrade"
)

func TestCollectorRestartArgsPreserveBackend(t *testing.T) {
	for _, tc := range []struct {
		backend string
		want    []string
	}{
		{"native", []string{"start", "--wait", "--config", "/custom space/collector.yaml", "--backend", "native"}},
		{"docker", []string{"start", "--wait", "--config", "/custom space/collector.yaml"}},
		{"", []string{"start", "--wait", "--config", "/custom space/collector.yaml"}},
	} {
		if got := collectorRestartArgs("/custom space/collector.yaml", tc.backend); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("backend=%q args=%q want=%q", tc.backend, got, tc.want)
		}
	}
}

func lifecycleFixture(t *testing.T) upgrade.Request {
	t.Helper()
	dir := t.TempDir()
	base := (&url.URL{Scheme: "file", Path: dir}).String()
	target := map[string]string{"darwin": "macos", "linux": "linux"}[runtime.GOOS] + "-" + map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	asset := filepath.Join(dir, "download", "v1.1.0", "lotel-"+target)
	if err := os.MkdirAll(filepath.Dir(asset), 0o755); err != nil {
		t.Fatal(err)
	}
	program := []byte("#!/bin/sh\necho 'lotel v1.1.0'\n")
	if err := os.WriteFile(asset, program, 0o755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(program)
	manifest := upgrade.Manifest{Schema: 1, Version: "1.1.0", Tag: "v1.1.0", Assets: map[string]string{target: base + "/download/v1.1.0/lotel-" + target}, SHA256: map[string]string{target: hex.EncodeToString(hash[:])}}
	data, _ := json.Marshal(manifest)
	latest := filepath.Join(dir, "latest", "download")
	if err := os.MkdirAll(latest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(latest, "lotel-manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lotel")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return upgrade.Request{Current: "v1.0.0", Executable: path, BaseURL: base}
}

func TestCollectorUpgradeLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name              string
		running, check    bool
		stopErr, startErr bool
		wantCalls         string
		wantErr           bool
	}{
		{"running", true, false, false, false, "status stop start", false},
		{"stopped", false, false, false, false, "status", false},
		{"check", true, true, false, false, "", false},
		{"stop fails", true, false, true, false, "status stop start", true},
		{"restart fails", true, false, false, true, "status stop start", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := lifecycleFixture(t)
			req.Check = tc.check
			var calls []string
			var notices bytes.Buffer
			control := collectorControl{
				running: func(context.Context) (bool, error) { calls = append(calls, "status"); return tc.running, nil },
				stop: func(context.Context) error {
					calls = append(calls, "stop")
					data, _ := os.ReadFile(req.Executable)
					if string(data) != "old" {
						t.Fatal("replaced before stop")
					}
					if !strings.Contains(notices.String(), "collection will be unavailable") {
						t.Fatal("no downtime warning before stop")
					}
					if tc.stopErr {
						return errors.New("stop failed")
					}
					return nil
				},
				start: func(ctx context.Context) error {
					calls = append(calls, "start")
					if ctx.Err() != nil {
						t.Fatal("recovery context already cancelled")
					}
					data, _ := os.ReadFile(req.Executable)
					if !tc.stopErr && string(data) == "old" {
						t.Fatal("restart did not use new binary")
					}
					if tc.startErr {
						return errors.New("restart failed")
					}
					return nil
				},
			}
			report, err := upgradeWithCollector(context.Background(), req, control, &notices)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if strings.Join(calls, " ") != tc.wantCalls {
				t.Fatal(calls)
			}
			if report.CollectorRestarted != (tc.running && !tc.check && !tc.startErr) {
				t.Fatalf("%+v", report)
			}
			if tc.startErr && !strings.Contains(fmt.Sprint(err), "start --wait") {
				t.Fatal("no recovery command")
			}
		})
	}
}

func TestValidationDoesNotPauseCollector(t *testing.T) {
	req := lifecycleFixture(t)
	u, _ := url.Parse(req.BaseURL)
	dataPath := filepath.Join(u.Path, "latest", "download", "lotel-manifest.json")
	if err := os.WriteFile(dataPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	control := collectorControl{running: func(context.Context) (bool, error) { t.Fatal("Collector touched before validation"); return false, nil }}
	if _, err := upgradeWithCollector(context.Background(), req, control, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestReplacementFailureRestartsCollector(t *testing.T) {
	req := lifecycleFixture(t)
	var restarted bool
	control := collectorControl{
		running: func(context.Context) (bool, error) { return true, nil },
		stop: func(context.Context) error {
			if err := os.Remove(req.Executable); err != nil {
				return err
			}
			return os.Mkdir(req.Executable, 0o755)
		},
		start: func(context.Context) error { restarted = true; return nil },
	}
	r, err := upgradeWithCollector(context.Background(), req, control, &bytes.Buffer{})
	if err == nil || r.Upgraded || !restarted {
		t.Fatalf("report=%+v err=%v restarted=%v", r, err, restarted)
	}
}

func TestCancelledUpgradeStillRestartsCollector(t *testing.T) {
	req := lifecycleFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restarted := false
	control := collectorControl{
		running: func(context.Context) (bool, error) { return true, nil },
		stop:    func(context.Context) error { cancel(); return context.Canceled },
		start: func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Fatal("cancelled recovery context")
			}
			restarted = true
			return nil
		},
	}
	if _, err := upgradeWithCollector(ctx, req, control, &bytes.Buffer{}); err == nil || !restarted {
		t.Fatalf("err=%v restarted=%v", err, restarted)
	}
}

func TestShellLiteralExecutesExactArguments(t *testing.T) {
	for _, value := range []string{"plain", "space path", "apostrophe's", "dollar$HOME", "`echo wrong`", "back\\slash", "line\nbreak", "trailing\n"} {
		command := exec.Command("sh", "-c", "printf '%s' "+shellLiteral(value))
		out, err := command.Output()
		if err != nil || string(out) != value {
			t.Fatalf("value=%q output=%q err=%v", value, out, err)
		}
	}
}
