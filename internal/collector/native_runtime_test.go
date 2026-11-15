package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattsp1290/lotel/internal/config"
)

func TestNativeCollectorValidatesManagedAndRecipeConfigs(t *testing.T) {
	temp := t.TempDir()
	managed := filepath.Join(temp, "managed.yaml")
	if err := os.WriteFile(managed, []byte(config.DefaultConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../../examples/collector/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range append(paths, managed) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := config.ReadNative(path, filepath.Join(temp, "data"))
			if err != nil {
				t.Fatal(err)
			}
			col, err := newNativeCollector(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := col.DryRun(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
