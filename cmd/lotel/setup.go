package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

type setupCandidate struct {
	Ecosystem string   `json:"ecosystem"`
	Manifests []string `json:"manifests"`
	Guide     string   `json:"guide"`
}

type setupSkipped struct {
	Manifest string `json:"manifest"`
	Reason   string `json:"reason"`
}

type setupReport struct {
	SchemaVersion int              `json:"schema_version"`
	Path          string           `json:"path"`
	Candidates    []setupCandidate `json:"candidates"`
	Skipped       []setupSkipped   `json:"skipped"`
	ExporterGuide string           `json:"exporter_guide"`
	Guidance      string           `json:"guidance"`
}

var setupManifests = []struct {
	name, ecosystem, guide string
}{
	{"Cargo.toml", "Rust", "https://opentelemetry.io/docs/languages/rust/"},
	{"build.gradle", "Java", "https://opentelemetry.io/docs/languages/java/"},
	{"build.gradle.kts", "Java", "https://opentelemetry.io/docs/languages/java/"},
	{"go.mod", "Go", "https://opentelemetry.io/docs/languages/go/"},
	{"package.json", "JavaScript", "https://opentelemetry.io/docs/languages/js/"},
	{"pom.xml", "Java", "https://opentelemetry.io/docs/languages/java/"},
	{"pyproject.toml", "Python", "https://opentelemetry.io/docs/languages/python/"},
	{"requirements.txt", "Python", "https://opentelemetry.io/docs/languages/python/"},
}

func setupCommand() *cobra.Command {
	var path string
	var recommendOnly bool
	cmd := &cobra.Command{
		Use: "setup", Short: "Recommend OpenTelemetry guides from project manifests (JSON)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !recommendOnly {
				return fmt.Errorf("--recommend-only is required")
			}
			if path == "" || !utf8.ValidString(path) {
				return fmt.Errorf("--path must be nonempty UTF-8")
			}
			directoryPath := path + string(os.PathSeparator) + "."
			if filepath.VolumeName(path) == path && !filepath.IsAbs(path) {
				directoryPath = path + "."
			}
			root, err := os.OpenRoot(directoryPath)
			if err != nil {
				return fmt.Errorf("inspect project directory: %w", err)
			}
			defer root.Close()
			report := setupReport{
				SchemaVersion: 1, Path: path, Candidates: []setupCandidate{}, Skipped: []setupSkipped{},
				ExporterGuide: "https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/",
				Guidance:      "If your SDK supports these environment variables and the managed Collector defaults are reachable from your application's network namespace, set OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 and OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf. Custom Collector configurations can use different endpoints.",
			}
			indices := map[string]int{}
			for _, manifest := range setupManifests {
				info, err := root.Lstat(manifest.name)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				reason := ""
				switch {
				case err != nil:
					reason = "unavailable"
				case info.Mode()&os.ModeSymlink != 0:
					reason = "symlink"
				case !info.Mode().IsRegular():
					reason = "not_regular"
				case info.Mode().Perm()&0444 == 0:
					reason = "no_read_permission_bits"
				}
				if reason != "" {
					report.Skipped = append(report.Skipped, setupSkipped{Manifest: manifest.name, Reason: reason})
					continue
				}
				index, found := indices[manifest.ecosystem]
				if !found {
					index = len(report.Candidates)
					indices[manifest.ecosystem] = index
					report.Candidates = append(report.Candidates, setupCandidate{Ecosystem: manifest.ecosystem, Guide: manifest.guide, Manifests: []string{}})
				}
				report.Candidates[index].Manifests = append(report.Candidates[index].Manifests, manifest.name)
			}
			sort.Slice(report.Candidates, func(i, j int) bool { return report.Candidates[i].Ecosystem < report.Candidates[j].Ecosystem })
			return writeJSON(cmd, report, true)
		},
	}
	cmd.Flags().BoolVar(&recommendOnly, "recommend-only", false, "inspect manifest names without changing files (required)")
	cmd.Flags().StringVar(&path, "path", ".", "project directory to inspect")
	return cmd
}
