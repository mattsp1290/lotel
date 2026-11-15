package collector_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/mattsp1290/lotel/internal/upgrade"
)

func TestNativeCLIUpgrade(t *testing.T) {
	if os.Getenv("LOTEL_NATIVE_TEST_BIN") == "" {
		t.Skip("set LOTEL_NATIVE_TEST_BIN to a built lotel executable")
	}
	base := nativeUpgradeRelease(t)
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprintf("running=%t", running), func(t *testing.T) {
			f := newNativeCLIFixture(t, "")
			original, err := os.ReadFile(f.bin)
			if err != nil {
				t.Fatal(err)
			}
			f.bin = filepath.Join(t.TempDir(), "installed lotel")
			if err := os.WriteFile(f.bin, original, 0o755); err != nil {
				t.Fatal(err)
			}
			f.env = append(f.env, "LOTEL_RELEASE_BASE_URL="+base)
			configPath := filepath.Join(f.dir, "lotel-collector.yaml")
			config, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			customConfig := filepath.Join(t.TempDir(), "custom collector.yaml")
			if err := os.WriteFile(customConfig, config, 0o600); err != nil {
				t.Fatal(err)
			}
			status := func() collector.Status {
				t.Helper()
				out, err := f.run("status")
				var status collector.Status
				if err != nil || json.Unmarshal(out, &status) != nil {
					t.Fatalf("status: %v\n%s", err, out)
				}
				return status
			}
			var before collector.Status
			if running {
				if out, err := f.run("start", "--backend", "native", "--wait", "--config", customConfig); err != nil {
					t.Fatalf("start: %v\n%s", err, out)
				}
				before = status()
				postUpgradeLog(t, f, "before replacement")
			}
			check, err := f.run("upgrade", "--check", "--force", "--version", "v1.1.0", "--json")
			var checked upgrade.Report
			if err != nil || json.Unmarshal(check, &checked) != nil || checked.Upgraded || checked.CollectorRestarted {
				t.Fatalf("check: %v\n%s", err, check)
			}
			unchanged, err := os.ReadFile(f.bin)
			if err != nil || sha256.Sum256(original) != sha256.Sum256(unchanged) {
				t.Fatalf("check changed executable: %v", err)
			}
			if running && status().PID != before.PID {
				t.Fatal("check restarted native Collector")
			}
			if running {
				statePath := filepath.Join(f.home, ".lotel", "collector.state")
				recorded, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				var state collector.State
				if err := json.Unmarshal(recorded, &state); err != nil {
					t.Fatal(err)
				}
				state.ControlToken = "wrong-upgrade-nonce"
				wrong, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(statePath, wrong, 0o600); err != nil {
					t.Fatal(err)
				}
				rejected, rejectedErr := f.run("upgrade", "--force", "--version", "v1.1.0", "--json")
				retained, readErr := os.ReadFile(statePath)
				if err := os.WriteFile(statePath, recorded, 0o600); err != nil {
					t.Fatal(err)
				}
				if rejectedErr == nil || !strings.Contains(string(rejected), "HTTP 401") {
					t.Fatalf("upgrade accepted wrong nonce: %v\n%s", rejectedErr, rejected)
				}
				if readErr != nil || string(retained) != string(wrong) {
					t.Fatalf("rejected upgrade changed recorded state: %v", readErr)
				}
				retained, err = os.ReadFile(f.bin)
				if err != nil || sha256.Sum256(retained) != sha256.Sum256(original) || status().PID != before.PID {
					t.Fatalf("rejected upgrade replaced binary or restarted Collector: %v", err)
				}
			}
			// Upgrade from another directory to prove it uses the recorded custom path.
			f.dir = t.TempDir()
			out, err := f.run("upgrade", "--force", "--version", "v1.1.0", "--json")
			// Restart notices and start output are on stderr, which run combines with JSON.
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			var report upgrade.Report
			if err != nil || json.Unmarshal([]byte(lines[len(lines)-1]), &report) != nil || !report.Upgraded || report.CollectorRestarted != running {
				t.Fatalf("upgrade: %v\n%s", err, out)
			}
			if out, err := f.run("--version"); err != nil || strings.TrimSpace(string(out)) != "lotel v1.1.0" {
				t.Fatalf("replacement version: %v\n%s", err, out)
			}
			if !running {
				if _, err := os.Stat(filepath.Join(f.home, ".lotel")); !os.IsNotExist(err) {
					t.Fatalf("stopped upgrade created Collector state: %v", err)
				}
				return
			}
			after := status()
			if !after.Running || !after.Healthy || after.Backend != "native" || after.PID == before.PID || after.ConfigPath != customConfig {
				t.Fatalf("restart changed backend/configuration: %+v -> %+v", before, after)
			}
			postUpgradeLog(t, f, "after replacement")
			if out, err := f.run("stop"); err != nil {
				t.Fatalf("stop upgraded Collector: %v\n%s", err, out)
			}
			out, err = f.run("query", "logs", "--service", "native-upgrade-test", "--since", "5m")
			var rows []json.RawMessage
			if err != nil || json.Unmarshal(out, &rows) != nil || len(rows) != 2 {
				t.Fatalf("telemetry did not survive upgrade/resume: %v\n%s", err, out)
			}
			unchanged, err = os.ReadFile(customConfig)
			if err != nil || string(unchanged) != string(config) {
				t.Fatalf("upgrade changed custom configuration: %v", err)
			}
		})
	}
}

func nativeUpgradeRelease(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	base := (&url.URL{Scheme: "file", Path: dir}).String()
	osName := map[string]string{"darwin": "macos", "linux": "linux"}[runtime.GOOS]
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if osName == "" || arch == "" {
		t.Skip("release upgrades require a supported Linux/macOS target")
	}
	target := osName + "-" + arch
	releaseDir := filepath.Join(dir, "download", "v1.1.0")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(releaseDir, "lotel-"+target)
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version=v1.1.0 -X main.distribution=release", "-o", asset, "./cmd/lotel")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build isolated release: %v\n%s", err, out)
	}
	binary, err := os.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	manifest := upgrade.Manifest{Schema: 1, Version: "1.1.0", Tag: "v1.1.0", Assets: map[string]string{target: base + "/download/v1.1.0/lotel-" + target}, SHA256: map[string]string{target: hex.EncodeToString(digest[:])}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "lotel-manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

func postUpgradeLog(t *testing.T, f nativeCLIFixture, body string) {
	t.Helper()
	payload := fmt.Sprintf(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"native-upgrade-test"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":%q,"body":{"stringValue":%q}}]}]}]}`, fmt.Sprint(time.Now().UnixNano()), body)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post(f.endpoint+"/v1/logs", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("OTLP logs: %s", response.Status)
	}
}
