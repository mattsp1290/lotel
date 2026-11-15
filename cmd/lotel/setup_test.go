package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func runSetup(t *testing.T, args ...string) (setupReport, []byte, error) {
	t.Helper()
	cmd := setupCommand()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	var report setupReport
	if err == nil {
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("invalid JSON: %v: %s", err, out.String())
		}
	}
	return report, out.Bytes(), err
}

func TestSetupRecommendations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("setup recommendations are unsupported on Windows")
	}
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	secret := "manifest-content-must-not-appear"
	for _, name := range []string{"go.mod", "package.json", "pom.xml", "build.gradle", "pyproject.toml"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "lotel-collector.yaml"), []byte("custom endpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, "Cargo.toml"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, "go.mod"), filepath.Join(project, "requirements.txt")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
	}
	first, firstBytes, err := runSetup(t, "--recommend-only", "--path", project)
	if err != nil {
		t.Fatal(err)
	}
	_, secondBytes, err := runSetup(t, "--recommend-only", "--path", project)
	if err != nil || !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("nondeterministic output: %v", err)
	}
	want := []setupCandidate{
		{Ecosystem: "Go", Manifests: []string{"go.mod"}, Guide: "https://opentelemetry.io/docs/languages/go/"},
		{Ecosystem: "Java", Manifests: []string{"build.gradle", "pom.xml"}, Guide: "https://opentelemetry.io/docs/languages/java/"},
		{Ecosystem: "JavaScript", Manifests: []string{"package.json"}, Guide: "https://opentelemetry.io/docs/languages/js/"},
		{Ecosystem: "Python", Manifests: []string{"pyproject.toml"}, Guide: "https://opentelemetry.io/docs/languages/python/"},
	}
	if !reflect.DeepEqual(first.Candidates, want) || first.SchemaVersion != 1 || first.Path != project {
		t.Fatalf("unexpected report: %s", firstBytes)
	}
	if len(first.Skipped) == 0 || first.Skipped[0] != (setupSkipped{Manifest: "Cargo.toml", Reason: "not_regular"}) {
		t.Fatalf("missing nonregular entry: %s", firstBytes)
	}
	if _, err := os.Lstat(filepath.Join(project, "requirements.txt")); err == nil && (len(first.Skipped) != 2 || first.Skipped[1] != (setupSkipped{Manifest: "requirements.txt", Reason: "symlink"})) {
		t.Fatalf("missing symlink entry: %s", firstBytes)
	}
	if strings.Contains(string(firstBytes), secret) || !strings.Contains(first.Guidance, "Custom Collector configurations") || !strings.Contains(first.Guidance, "network namespace") {
		t.Fatalf("report leaked content or omitted endpoint conditions: %s", firstBytes)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("setup wrote to HOME: %v %v", entries, err)
	}
}

func TestSetupBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("setup recommendations are unsupported on Windows")
	}
	project := t.TempDir()
	child := filepath.Join(project, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "go.mod"), []byte("module example"), 0600); err != nil {
		t.Fatal(err)
	}
	rootReport, _, err := runSetup(t, "--recommend-only", "--path", project)
	if err != nil || len(rootReport.Candidates) != 0 || rootReport.Candidates == nil || rootReport.Skipped == nil || rootReport.ExporterGuide == "" {
		t.Fatalf("empty root: %+v %v", rootReport, err)
	}
	childReport, _, err := runSetup(t, "--recommend-only", "--path", child)
	if err != nil || len(childReport.Candidates) != 1 || childReport.Candidates[0].Ecosystem != "Go" {
		t.Fatalf("child: %+v %v", childReport, err)
	}
	for _, args := range [][]string{{}, {"--recommend-only", "--path", ""}, {"--recommend-only", "--path", "\xff"}, {"--recommend-only", "--path", filepath.Join(project, "missing")}, {"--recommend-only", "--path", filepath.Join(child, "go.mod")}} {
		if _, _, err := runSetup(t, args...); err == nil {
			t.Errorf("expected invalid args to fail: %q", args)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(child, "go.mod"), 0000); err != nil {
			t.Fatal(err)
		}
		report, _, err := runSetup(t, "--recommend-only", "--path", child)
		if err != nil || len(report.Candidates) != 0 || !reflect.DeepEqual(report.Skipped, []setupSkipped{{Manifest: "go.mod", Reason: "no_read_permission_bits"}}) {
			t.Fatalf("permission bits: %+v %v", report, err)
		}
	}
}
