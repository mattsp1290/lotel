package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestNativeConfigPathsAndIngestion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "collector.yaml")
	input := DefaultConfig + "\ningestion:\n  enabled: true\n  interval: 250ms\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadNative(path, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Ingestion.Enabled || cfg.Ingestion.Interval != 250*time.Millisecond {
		t.Fatalf("options: %+v", cfg.Ingestion)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(cfg.YAML, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["ingestion"]; ok {
		t.Fatal("lotel configuration leaked into Collector YAML")
	}
	exporters := doc["exporters"].(map[string]any)
	if exporters["file/traces"].(map[string]any)["path"] != filepath.Join(dir, "data", "traces", "traces.jsonl") {
		t.Fatalf("paths: %v", exporters)
	}
	if !strings.Contains(string(cfg.YAML), "gen_ai_normalizer") || !strings.Contains(string(cfg.YAML), "spanmetrics") {
		t.Fatal("managed pipeline changed")
	}
}

func TestNativeConfigPreservesCustomValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "collector.yaml")
	input := `exporters:
  file/traces:
    path: /custom/traces.jsonl
  otlp:
    endpoint: /data/unchanged
processors:
  transform:
    trace_statements: ['/data/unchanged']
ingestion:
  enabled: false
`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadNative(path, "/host")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ingestion.Enabled {
		t.Fatal("disabled ingestion became enabled")
	}
	if strings.Count(string(cfg.YAML), "/data/unchanged") != 2 || !strings.Contains(string(cfg.YAML), "/custom/traces.jsonl") {
		t.Fatalf("custom values changed: %s", cfg.YAML)
	}
}

func TestNativeConfigRejectsInvalidIngestion(t *testing.T) {
	for _, option := range []string{"interval: 0s", "interval: -1s", "interval: nope", "enabled: nonsense", "unknown: true"} {
		t.Run(option, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "c.yaml")
			if err := os.WriteFile(path, []byte("ingestion:\n  "+option+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadNative(path, "/host"); err == nil {
				t.Fatal("invalid ingestion configuration accepted")
			}
		})
	}
}

func TestCollectorConfigPreservesDockerValuesAndDefaultsIngestion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.yaml")
	input := `receivers:
  prometheus:
    config:
      scrape_configs: []
exporters:
  file/traces:
    path: /data/traces/traces.jsonl
service: {}
`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadCollector(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Ingestion.Enabled || cfg.Ingestion.Interval != 2*time.Second {
		t.Fatalf("defaults: %+v", cfg.Ingestion)
	}
	if !strings.Contains(string(cfg.YAML), "/data/traces/traces.jsonl") || !strings.Contains(string(cfg.YAML), "prometheus:") {
		t.Fatalf("Docker config was adapted as native config: %s", cfg.YAML)
	}
	if err := os.WriteFile(path, []byte(input+"ingestion:\n  enabled: false\n  interval: 5s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = ReadCollector(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ingestion.Enabled || cfg.Ingestion.Interval != 5*time.Second || strings.Contains(string(cfg.YAML), "ingestion:") {
		t.Fatalf("lotel settings leaked into Docker config: %+v\n%s", cfg.Ingestion, cfg.YAML)
	}
}
