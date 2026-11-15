package config

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testCollectorImage = "otel/opentelemetry-collector-contrib:0.165.0"

func dockerTests(t *testing.T) string {
	t.Helper()
	if os.Getenv("LOTEL_DOCKER_TESTS") != "1" {
		t.Skip("set LOTEL_DOCKER_TESTS=1 for collector tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker unavailable")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".lotel", "tmp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	temp, err := os.MkdirTemp(dir, "recipe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(temp) })
	return temp
}

func TestExampleConfigsValidate(t *testing.T) {
	temp := dockerTests(t)
	defaultPath := filepath.Join(temp, "default.yaml")
	if err := os.WriteFile(defaultPath, []byte(DefaultConfig), 0644); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../../examples/collector/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, defaultPath)
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command("docker", "run", "--rm", "-v", absolute+":/c.yaml:ro", testCollectorImage, "validate", "--config=/c.yaml", "--feature-gates=+service.profilesSupport").CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", path, err, output)
		}
	}
}

func TestRedactionRecipesBehavior(t *testing.T) {
	temp := dockerTests(t)
	for _, recipe := range []string{"redact", "hash"} {
		t.Run(recipe, func(t *testing.T) {
			data := filepath.Join(temp, recipe)
			for _, signal := range []string{"traces", "metrics", "logs"} {
				dir := filepath.Join(data, signal)
				if err := os.MkdirAll(dir, 0777); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0777); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := filepath.Abs("../../examples/collector/genai-" + recipe + "-content.yaml")
			if err != nil {
				t.Fatal(err)
			}
			name := "lotel-recipe-test"
			output, err := exec.Command("docker", "run", "-d", "--name", name, "-p", "14318:4318", "-p", "14317:4317", "-v", cfg+":/etc/otelcol-contrib/config.yaml:ro", "-v", data+":/data", testCollectorImage).CombinedOutput()
			if err != nil {
				t.Fatalf("start: %v %s", err, output)
			}
			defer exec.Command("docker", "rm", "-f", name).Run()
			client := &http.Client{Timeout: time.Second}
			deadline := time.Now().Add(15 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				resp, err := client.Get("http://localhost:14318/")
				if err == nil {
					resp.Body.Close()
					ready = true
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if !ready {
				t.Fatal("recipe collector did not start")
			}
			nonce := fmt.Sprintf("recipe-secret-%d", time.Now().UnixNano())
			for _, signal := range []string{"traces", "logs"} {
				content, err := os.ReadFile(filepath.Join("../storage/testdata/genai", signal+".jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				content = bytes.ReplaceAll(content, []byte("fixture prompt"), []byte(nonce))
				resp, err := client.Post("http://localhost:14318/v1/"+signal, "application/json", bytes.NewReader(content))
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatalf("send %s: %d", signal, resp.StatusCode)
				}
			}
			deadline = time.Now().Add(15 * time.Second)
			for {
				complete := true
				for _, signal := range []string{"traces", "logs"} {
					content, err := os.ReadFile(filepath.Join(data, signal, signal+".jsonl"))
					if err != nil || len(content) == 0 {
						complete = false
						continue
					}
					if bytes.Contains(content, []byte(nonce)) {
						t.Fatalf("%s leaked content", signal)
					}
					if signal == "traces" && !strings.Contains(string(content), "gen_ai.usage.input_tokens") {
						t.Fatal("tokens removed")
					}
				}
				if complete {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("recipe telemetry not exported")
				}
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
}
