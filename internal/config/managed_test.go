package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedConfigRefresh(t *testing.T) {
	for _, tc := range []struct {
		name, content   string
		replace, backup bool
	}{
		{"absent", "", true, false},
		{"older", "# lotel-managed-config v1 old\n", true, false},
		{"previous", "# lotel-managed-config v2 previous\n", true, false},
		{"current", ManagedConfigHeader + "\ncustom", false, false},
		{"future", "# lotel-managed-config v4 future\n", false, false},
		{"legacy", "legacy config", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), DefaultConfigName)
			if tc.content != "" {
				if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var notices bytes.Buffer
			old := Notices
			Notices = &notices
			defer func() { Notices = old }()
			if err := refreshManagedConfig(path); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.content
			if tc.replace {
				want = DefaultConfig
			}
			if string(got) != want {
				t.Fatal("unexpected config content")
			}
			backups, _ := filepath.Glob(path + ".bak.*")
			if tc.backup {
				if len(backups) != 1 {
					t.Fatal("missing backup")
				}
				original, _ := os.ReadFile(backups[0])
				if string(original) != tc.content {
					t.Fatal("backup changed")
				}
				if !strings.Contains(notices.String(), "backup:") {
					t.Fatal("missing notice")
				}
			} else if len(backups) != 0 {
				t.Fatal("unexpected backup")
			}
			if tc.name == "future" && !strings.Contains(notices.String(), "newer") {
				t.Fatal("missing future-version notice")
			}
		})
	}
}
