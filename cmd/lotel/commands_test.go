package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandCatalogFlags(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	root.PersistentFlags().String("scope", "root", "root scope")
	root.PersistentFlags().Bool("shared", false, "shared switch")
	root.PersistentFlags().String("secret", "", "hidden value")
	if err := root.PersistentFlags().MarkHidden("secret"); err != nil {
		t.Fatal(err)
	}
	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{Use: "child ARG", Short: "A child", Run: func(*cobra.Command, []string) {}}
	child.Flags().String("scope", "child", "local scope")
	parent.AddCommand(child, &cobra.Command{Use: "private", Hidden: true})
	root.AddCommand(parent)

	catalog := buildCommandCatalog(root, "v1.2.3")
	if catalog.SchemaVersion != 1 || catalog.Version != "v1.2.3" || len(catalog.Commands) != 3 {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	entry := catalog.Commands[2]
	if entry.Path != "app parent child" || entry.Usage != "child ARG" || entry.Summary != "A child" || !entry.Runnable {
		t.Fatalf("unexpected command: %+v", entry)
	}
	want := []catalogFlag{
		{Name: "scope", Type: "string", Default: "child", Description: "local scope"},
		{Name: "shared", Type: "bool", Default: "false", Description: "shared switch", Inherited: true},
	}
	if !reflect.DeepEqual(entry.Flags, want) {
		t.Fatalf("flags = %+v, want %+v", entry.Flags, want)
	}
}

func TestCommandsCLI(t *testing.T) {
	home := t.TempDir()
	invoke := func() []byte {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelper$", "--", "commands")
		cmd.Env = append(os.Environ(), "HOME="+home, "LOTEL_CLI_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("commands failed: %v: %s", err, output)
		}
		return output
	}
	first, second := invoke(), invoke()
	if !bytes.Equal(first, second) {
		t.Fatal("catalog output changed between calls")
	}
	var catalog commandCatalog
	if err := json.Unmarshal(first, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 || catalog.Version == "" || len(catalog.Commands) == 0 {
		t.Fatalf("invalid catalog: %s", first)
	}
	paths := make([]string, 0, len(catalog.Commands))
	var root, trace *catalogCommand
	for i := range catalog.Commands {
		entry := &catalog.Commands[i]
		paths = append(paths, entry.Path)
		if entry.Flags == nil {
			t.Fatalf("nil flags for %s", entry.Path)
		}
		if strings.Contains(entry.Path, "run-collector") || strings.Contains(entry.Path, " completion") || entry.Path == "lotel help" {
			t.Fatalf("internal or implicit command appeared: %s", entry.Path)
		}
		if entry.Path == "lotel query traces" {
			trace = entry
		}
		if entry.Path == "lotel" {
			root = entry
		}
		for _, flag := range entry.Flags {
			if flag.Name == "help" {
				t.Fatalf("generated help flag appeared in %s", entry.Path)
			}
		}
	}
	if !sort.StringsAreSorted(paths) || root == nil || trace == nil || !trace.Runnable {
		t.Fatalf("missing trace query or unsorted paths: %v", paths)
	}
	versionFound := false
	for _, flag := range root.Flags {
		if flag.Name == "version" && flag.Shorthand == "v" && flag.Type == "bool" && flag.Default == "false" && !flag.Inherited {
			versionFound = true
		}
	}
	if !versionFound {
		t.Fatalf("missing root version flag: %+v", root.Flags)
	}
	found := false
	for _, flag := range trace.Flags {
		if flag.Name == "errors-only" && flag.Type == "bool" && flag.Default == "false" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing trace flag: %+v", trace.Flags)
	}
	if strings.Contains(string(first), `"token"`) {
		t.Fatal("internal token appeared in catalog")
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("catalog touched telemetry state: %v", err)
	}
}
