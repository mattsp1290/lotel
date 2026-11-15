package collector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/config"
)

func generatedDockerConfig(t *testing.T, source string, existing bool) string {
	t.Helper()
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	argsPath := filepath.Join(home, "args")
	t.Setenv("LOTEL_TEST_DOCKER_ARGS", argsPath)
	script := "#!/bin/sh\ncase \"$1\" in\ninspect) exit 1;;\nps) exit 0;;\nrun) printf '%s\\n' \"$@\" > \"$LOTEL_TEST_DOCKER_ARGS\"; printf container;;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	privateDir := filepath.Join(home, stateDir, "docker-config")
	generated := filepath.Join(privateDir, "collector.yaml")
	if existing {
		if err := os.MkdirAll(privateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(generated, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, "source.yaml")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := startDocker(t.Context(), path, filepath.Join(home, "data")); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil || !strings.Contains(string(args), generated+":/etc/otelcol-contrib/config.yaml:ro\n") {
		t.Fatalf("generated file not mounted: %s %v", args, err)
	}
	for path, want := range map[string]os.FileMode{privateDir: 0o700, generated: 0o644, path: 0o600, filepath.Join(home, stateDir, stateFile): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("permissions for %s: %v %v, want %o", path, info, err, want)
		}
	}
	original, err := os.ReadFile(path)
	if err != nil || string(original) != source {
		t.Fatalf("changed source configuration: %s %v", original, err)
	}
	return generated
}

func TestDockerGeneratedConfigPermissions(t *testing.T) {
	for _, source := range []string{config.DefaultConfig, config.DefaultConfig + "\ningestion:\n  interval: 25ms\n"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("ingestion=%t/existing=%t", strings.Contains(source, "ingestion:"), existing), func(t *testing.T) {
				generatedDockerConfig(t, source, existing)
			})
		}
	}
}

func TestDockerGeneratedConfigLinuxValidation(t *testing.T) {
	if os.Getenv("LOTEL_DOCKER_PERMISSION_TEST") != "1" {
		t.Skip("set LOTEL_DOCKER_PERMISSION_TEST=1 for isolated Linux-volume validation")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{config.DefaultConfig, config.DefaultConfig + "\ningestion:\n  interval: 25ms\n"} {
		t.Run(fmt.Sprintf("ingestion=%t", strings.Contains(source, "ingestion:")), func(t *testing.T) {
			generated := generatedDockerConfig(t, source, true)
			data, err := os.ReadFile(generated)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(generated)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, docker, "volume", "create").Output()
			if err != nil {
				t.Fatal(err)
			}
			volume := strings.TrimSpace(string(out))
			defer func() {
				if out, err := exec.Command(docker, "volume", "rm", volume).CombinedOutput(); err != nil {
					t.Errorf("remove isolated volume: %v %s", err, out)
				}
			}()
			populate := exec.CommandContext(ctx, docker, "run", "--rm", "--pull=never", "-i", "-v", volume+":/probe", "alpine:latest", "sh", "-c", "cat > /probe/config.yaml && chown 501:501 /probe/config.yaml && chmod 600 /probe/config.yaml")
			populate.Stdin = bytes.NewReader(data)
			if out, err := populate.CombinedOutput(); err != nil {
				t.Fatalf("populate Linux file: %v %s", err, out)
			}
			validate := func() ([]byte, error) {
				return exec.CommandContext(ctx, docker, "run", "--rm", "--pull=never", "-v", volume+":/probe:ro", defaultImage, "validate", "--config=/probe/config.yaml", "--feature-gates=+service.profilesSupport").CombinedOutput()
			}
			if out, err := validate(); err == nil || !strings.Contains(string(out), "permission denied") {
				t.Fatalf("0600 control did not fail on permissions: %v %s", err, out)
			}
			if out, err := exec.CommandContext(ctx, docker, "run", "--rm", "--pull=never", "-v", volume+":/probe", "alpine:latest", "chmod", fmt.Sprintf("%o", info.Mode().Perm()), "/probe/config.yaml").CombinedOutput(); err != nil {
				t.Fatalf("apply generated file mode: %v %s", err, out)
			}
			if out, err := validate(); err != nil {
				t.Fatalf("generated config failed Linux validation: %v %s", err, out)
			}
		})
	}
}
