package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattsp1290/lotel/internal/ingestion"
	"go.yaml.in/yaml/v3"
)

type CollectorConfig struct {
	YAML      []byte
	Ingestion ingestion.Options
}

func ReadCollector(path string) (CollectorConfig, error) {
	result := CollectorConfig{Ingestion: ingestion.Options{Enabled: true, Interval: 2 * time.Second}}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return result, fmt.Errorf("parsing collector config: %w", err)
	}
	if value, ok := document["ingestion"]; ok {
		encoded, err := yaml.Marshal(value)
		if err != nil {
			return result, err
		}
		options := struct {
			Enabled  bool   `yaml:"enabled"`
			Interval string `yaml:"interval"`
		}{true, "2s"}
		decoder := yaml.NewDecoder(strings.NewReader(string(encoded)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&options); err != nil {
			return result, fmt.Errorf("invalid ingestion config: %w", err)
		}
		interval, err := time.ParseDuration(options.Interval)
		if err != nil || interval <= 0 {
			return result, fmt.Errorf("ingestion.interval must be a positive duration, got %q", options.Interval)
		}
		result.Ingestion = ingestion.Options{Enabled: options.Enabled, Interval: interval}
		delete(document, "ingestion")
	}
	result.YAML, err = yaml.Marshal(document)
	return result, err
}

func ReadNative(path, dataPath string) (CollectorConfig, error) {
	result, err := ReadCollector(path)
	if err != nil {
		return result, err
	}
	var document map[string]any
	if err := yaml.Unmarshal(result.YAML, &document); err != nil {
		return result, err
	}
	if exporters, ok := document["exporters"].(map[string]any); ok {
		for name, value := range exporters {
			if name != "file" && !strings.HasPrefix(name, "file/") {
				continue
			}
			settings, ok := value.(map[string]any)
			if !ok {
				continue
			}
			path, ok := settings["path"].(string)
			if !ok {
				continue
			}
			if path == "/data" {
				settings["path"] = dataPath
			} else if strings.HasPrefix(path, "/data/") {
				settings["path"] = filepath.Join(dataPath, strings.TrimPrefix(path, "/data/"))
			}
		}
	}
	result.YAML, err = yaml.Marshal(document)
	return result, err
}
