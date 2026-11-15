package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathsPreservesProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(original) })
	path := filepath.Join(cwd, "lotel-collector.yaml")
	const content = "receivers:\n  custom: {}\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonical {
		t.Fatalf("resolved config %q, want %q", got, canonical)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("project config changed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, LotelDir, DefaultConfigName)); !os.IsNotExist(err) {
		t.Fatalf("home config was created despite project config: %v", err)
	}
}

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
	for _, sub := range []string{"traces", "metrics", "logs", "profiles"} {
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

func TestExplicitConfigPreservesSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	custom := filepath.Join(project, "custom.yaml")
	if err := os.WriteFile(custom, []byte("custom config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, _, err := ResolvePathsWithConfig(custom)
	if err != nil || path != custom {
		t.Fatalf("path=%q err=%v", path, err)
	}
	got, _ := os.ReadFile(custom)
	if string(got) != "custom config\n" {
		t.Fatal("custom config modified")
	}
	managed := filepath.Join(home, LotelDir, DefaultConfigName)
	if err := os.WriteFile(managed, []byte("# lotel-managed-config v1\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, _, err = ResolvePathsWithConfig(managed)
	if err != nil || path != managed {
		t.Fatalf("path=%q err=%v", path, err)
	}
	got, _ = os.ReadFile(managed)
	if string(got) != DefaultConfig {
		t.Fatal("managed defaults did not refresh")
	}
	if _, _, err = ResolvePathsWithConfig(filepath.Join(project, "missing.yaml")); err == nil {
		t.Fatal("missing explicit config accepted")
	}
}
