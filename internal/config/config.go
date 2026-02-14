package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// LotelDir is the base directory for all lotel state and data.
const LotelDir = ".lotel"

// DefaultConfigName is the embedded config file written when no custom config is found.
const DefaultConfigName = "collector-config.yaml"

// DefaultConfig is the minimal collector configuration for file-based exports.
const DefaultConfig = `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024
    send_batch_max_size: 2048

exporters:
  file/traces:
    path: /data/traces/traces.jsonl
    format: json
  file/metrics:
    path: /data/metrics/metrics.jsonl
    format: json
  file/logs:
    path: /data/logs/logs.jsonl
    format: json

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/traces]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/metrics]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/logs]
  telemetry:
    logs:
      level: info
`

// ResolvePaths returns the config file path and data directory path.
// It looks for a config in the current working directory first, then
// falls back to a default config in ~/.lotel/.
func ResolvePaths() (configPath, dataPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("getting home directory: %w", err)
	}

	dataPath = filepath.Join(home, LotelDir, "data")
	for _, sub := range []string{"traces", "metrics", "logs"} {
		if err := os.MkdirAll(filepath.Join(dataPath, sub), 0o755); err != nil {
			return "", "", fmt.Errorf("creating data subdirectory %s: %w", sub, err)
		}
	}

	// Look for config in CWD first (project-local config).
	cwd, err := os.Getwd()
	if err == nil {
		candidate := filepath.Join(cwd, "lotel-collector.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, dataPath, nil
		}
	}

	// Fall back to ~/.lotel/collector-config.yaml, creating it with defaults if absent.
	lotelDir := filepath.Join(home, LotelDir)
	if err := os.MkdirAll(lotelDir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating lotel directory: %w", err)
	}
	configPath = filepath.Join(lotelDir, DefaultConfigName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(DefaultConfig), 0o644); err != nil {
			return "", "", fmt.Errorf("writing default config: %w", err)
		}
	}

	return configPath, dataPath, nil
}

// DataPath returns the data directory path.
func DataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, LotelDir, "data"), nil
}
