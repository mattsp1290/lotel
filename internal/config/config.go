package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	ImageName     = "otel/opentelemetry-collector-contrib:latest"
	ContainerName = "telemetry-nest-otel-collector"
	NetworkName   = "telemetry-nest-network"
)

var Ports = []string{"4317", "4318", "13133", "8889"}

func ResolvePaths() (configPath, dataPath string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("getting working directory: %w", err)
	}

	configPath = filepath.Join(cwd, "docker", "configs", "otel", "otel-collector-config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		return "", "", fmt.Errorf("config file not found: %s", configPath)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("getting home directory: %w", err)
	}
	dataPath = filepath.Join(home, ".lotel", "data")

	for _, sub := range []string{"traces", "metrics", "logs"} {
		if err := os.MkdirAll(filepath.Join(dataPath, sub), 0o755); err != nil {
			return "", "", fmt.Errorf("creating data subdirectory %s: %w", sub, err)
		}
	}

	return configPath, dataPath, nil
}
