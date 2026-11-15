package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, tag, program string) (string, Manifest) {
	t.Helper()
	dir := t.TempDir()
	base := (&url.URL{Scheme: "file", Path: dir}).String()
	target, err := platformTarget()
	if err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(dir, "download", tag, "lotel-"+target)
	if err := os.MkdirAll(filepath.Dir(asset), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(program))
	m := Manifest{1, strings.TrimPrefix(tag, "v"), tag, map[string]string{target: base + "/download/" + tag + "/lotel-" + target}, map[string]string{target: hex.EncodeToString(h[:])}}
	writeManifest(t, dir, m)
	return base, m
}

func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "latest", "download", "lotel-manifest.json"), filepath.Join(dir, "download", m.Tag, "lotel-manifest.json")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func executable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lotel")
	if err := os.WriteFile(path, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpgradeBehavior(t *testing.T) {
	for _, tc := range []struct {
		name, current, tag   string
		check, source, force bool
		pin                  string
		wantError, upgraded  bool
	}{
		{"newer", "v1.0.0", "v1.1.0", false, false, false, "", false, true},
		{"check", "v1.0.0", "v1.1.0", true, false, false, "", false, false},
		{"same", "v1.1.0", "v1.1.0", false, false, false, "", false, false},
		{"no downgrade", "v2.0.0", "v1.1.0", false, false, false, "", false, false},
		{"pinned rollback", "v2.0.0", "v1.1.0", false, false, false, "v1.1.0", false, true},
		{"source rejected", "v1.0.0", "v1.1.0", false, true, false, "", true, false},
		{"pinned source rejected", "dev", "v1.1.0", false, true, false, "v1.1.0", true, false},
		{"forced source", "dev", "v1.1.0", false, true, true, "", false, true},
		{"source check", "dev", "v1.1.0", true, true, false, "", false, false},
		{"unknown release rejected", "dev", "v1.1.0", false, false, false, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program := "#!/bin/sh\nprintf 'lotel " + tc.tag + "\\n'\n"
			base, _ := fixture(t, tc.tag, program)
			path := executable(t)
			hook := false
			report, err := Run(context.Background(), Request{Current: tc.current, Executable: path, BaseURL: base, Version: tc.pin, Check: tc.check, SourceBuild: tc.source, Force: tc.force, BeforeReplace: func(context.Context) error { hook = true; return nil }})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if report.Upgraded != tc.upgraded || hook != tc.upgraded {
				t.Fatalf("report=%+v hook=%v", report, hook)
			}
			got, _ := os.ReadFile(path)
			want := "original"
			if tc.upgraded {
				want = program
			}
			if string(got) != want {
				t.Fatalf("executable changed unexpectedly: %q", got)
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".lotel-upgrade-*"))
			if len(files) != 0 {
				t.Fatalf("temporary files: %v", files)
			}
		})
	}
}

func TestValidationPreservesExecutable(t *testing.T) {
	for _, fault := range []string{"schema", "tag", "url", "digest", "content", "version", "pin", "hook"} {
		t.Run(fault, func(t *testing.T) {
			program := "#!/bin/sh\nprintf 'lotel v1.1.0\\n'\n"
			if fault == "version" {
				program = "#!/bin/sh\nprintf 'wrong\\n'\n"
			}
			base, m := fixture(t, "v1.1.0", program)
			dir, _ := url.Parse(base)
			target, _ := platformTarget()
			req := Request{Current: "v1.0.0", Executable: executable(t), BaseURL: base}
			switch fault {
			case "schema":
				m.Schema = 2
			case "tag":
				m.Version = "01.1.0"
			case "url":
				m.Assets[target] = "https://example.com/other"
			case "digest":
				m.SHA256[target] = "bad"
			case "content":
				m.SHA256[target] = strings.Repeat("0", 64)
			case "pin":
				req.Version = "v1.0.0"
			case "hook":
				req.BeforeReplace = func(context.Context) error { return os.ErrPermission }
			}
			writeManifest(t, dir.Path, m)
			if _, err := Run(context.Background(), req); err == nil {
				t.Fatal("expected validation error")
			}
			got, _ := os.ReadFile(req.Executable)
			if string(got) != "original" {
				t.Fatal("executable changed")
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(req.Executable), ".lotel-upgrade-*"))
			if len(files) != 0 {
				t.Fatal(files)
			}
		})
	}
}

func TestSymlinkUpgrade(t *testing.T) {
	base, _ := fixture(t, "v1.1.0", "#!/bin/sh\necho 'lotel v1.1.0'\n")
	path := executable(t)
	link := filepath.Join(t.TempDir(), "linked-lotel")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	r, err := Run(context.Background(), Request{Current: "v1.0.0", Executable: link, BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(path)
	if !r.Upgraded || r.Path != resolved {
		t.Fatalf("%+v", r)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
}

func TestInvalidRequest(t *testing.T) {
	for _, req := range []Request{{Version: "v01.2.3"}, {BaseURL: "http://example.com"}, {BaseURL: "file://remote/tmp"}, {BaseURL: "https://example.com?query=true"}} {
		if _, err := Run(context.Background(), req); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}

func TestVersionProbeBoundsDescendantOutput(t *testing.T) {
	base, _ := fixture(t, "v1.1.0", "#!/bin/sh\necho 'lotel v1.1.0'\nsleep 10 &\n")
	path := executable(t)
	_, err := Run(context.Background(), Request{Current: "v1.0.0", Executable: path, BaseURL: base})
	if err == nil {
		t.Fatal("candidate with open descendant output accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatal("executable changed")
	}
}
