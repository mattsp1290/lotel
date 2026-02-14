package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePaths_DefaultConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Change to a temp CWD with no config files.
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	configPath, dataPath, err := ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	// Should create default config in ~/.lotel/.
	expectedConfig := filepath.Join(tmp, LotelDir, DefaultConfigName)
	if configPath != expectedConfig {
		t.Errorf("configPath = %q, want %q", configPath, expectedConfig)
	}

	// Default config file should exist and have content.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if len(data) == 0 {
		t.Error("default config is empty")
	}

	// Data path should be ~/.lotel/data.
	expectedData := filepath.Join(tmp, LotelDir, "data")
	if dataPath != expectedData {
		t.Errorf("dataPath = %q, want %q", dataPath, expectedData)
	}

	// Data subdirectories should exist.
	for _, sub := range []string{"traces", "metrics", "logs"} {
		dir := filepath.Join(dataPath, sub)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("data subdirectory %s does not exist", sub)
		}
	}
}

func TestDataPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dp, err := DataPath()
	if err != nil {
		t.Fatalf("DataPath: %v", err)
	}
	expected := filepath.Join(tmp, LotelDir, "data")
	if dp != expected {
		t.Errorf("DataPath = %q, want %q", dp, expected)
	}
}
