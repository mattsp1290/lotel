//go:build !windows

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestSignalWALWriterHelper(t *testing.T) {
	path := os.Getenv("LOTEL_SIGNAL_WAL_WRITER")
	if path == "" {
		return
	}
	db, err := storage.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO logs (timestamp,service_name,date) VALUES ('2025-01-01 00:00:00','app','2025-01-01')`); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestDiagnoseSignalsRejectsWALsCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lotel")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	for _, kind := range []string{"corrupt", "valid", "symlink-unrelated", "symlink-dangling", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, ".lotel", "data")
			path := filepath.Join(dir, "lotel.db")
			initialize := func(p string) {
				t.Helper()
				if err := storage.WithDB(context.Background(), p, func(db *sql.DB) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			writeWAL := func(p string) {
				t.Helper()
				c := exec.Command(os.Args[0], "-test.run=^TestSignalWALWriterHelper$")
				c.Env = append(os.Environ(), "LOTEL_SIGNAL_WAL_WRITER="+p)
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("WAL writer: %v: %s", err, out)
				}
			}
			initialize(path)
			wal := path + ".wal"
			var otherWAL string
			switch kind {
			case "corrupt":
				if err := os.WriteFile(wal, []byte("not a valid WAL"), 0600); err != nil {
					t.Fatal(err)
				}
			case "valid":
				writeWAL(path)
			case "symlink-unrelated":
				other := filepath.Join(dir, "other.db")
				initialize(other)
				writeWAL(other)
				otherWAL = other + ".wal"
				if err := os.Symlink(otherWAL, wal); err != nil {
					t.Fatal(err)
				}
			case "symlink-dangling":
				if err := os.Symlink(filepath.Join(dir, "absent.wal"), wal); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(wal, 0600); err != nil {
					t.Fatal(err)
				}
			}
			dbBefore, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeInfo, err := os.Lstat(wal)
			if err != nil {
				t.Fatal(err)
			}
			var beforeContent []byte
			if beforeInfo.Mode().IsRegular() {
				beforeContent, err = os.ReadFile(wal)
				if err != nil {
					t.Fatal(err)
				}
			}
			var beforeTarget string
			if beforeInfo.Mode()&os.ModeSymlink != 0 {
				beforeTarget, err = os.Readlink(wal)
				if err != nil {
					t.Fatal(err)
				}
			}
			var otherBefore []byte
			if otherWAL != "" {
				otherBefore, err = os.ReadFile(otherWAL)
				if err != nil {
					t.Fatal(err)
				}
			}
			entriesBefore, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			namesBefore := make([]string, len(entriesBefore))
			for i, e := range entriesBefore {
				namesBefore[i] = e.Name()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "diagnose", "signals", "--service", "app", "--since", "1h")
			cmd.Env = append(os.Environ(), "HOME="+home)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("diagnose: %v: %s", err, out)
			}
			var report storage.SignalDiagnosis
			if err = json.Unmarshal(out, &report); err != nil || report.Observation != "unavailable" || report.Reason != "wal_present" {
				t.Fatalf("report: %s %v", out, err)
			}
			for _, signal := range report.Signals {
				if signal.ObservedInWindow != nil || signal.LatestInWindow != nil {
					t.Fatalf("false evidence: %+v", signal)
				}
			}
			dbAfter, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(dbBefore) != sha256.Sum256(dbAfter) {
				t.Fatal("database changed")
			}
			afterInfo, err := os.Lstat(wal)
			if err != nil || afterInfo.Mode() != beforeInfo.Mode() {
				t.Fatalf("WAL changed: %v", err)
			}
			if beforeInfo.Mode().IsRegular() {
				after, err := os.ReadFile(wal)
				if err != nil || !reflect.DeepEqual(after, beforeContent) {
					t.Fatalf("WAL content changed: %v", err)
				}
			}
			if beforeInfo.Mode()&os.ModeSymlink != 0 {
				after, err := os.Readlink(wal)
				if err != nil || after != beforeTarget {
					t.Fatalf("WAL link changed: %v", err)
				}
			}
			if otherWAL != "" {
				after, err := os.ReadFile(otherWAL)
				if err != nil || !reflect.DeepEqual(after, otherBefore) {
					t.Fatalf("external WAL changed: %v", err)
				}
			}
			entriesAfter, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			namesAfter := make([]string, len(entriesAfter))
			for i, e := range entriesAfter {
				namesAfter[i] = e.Name()
			}
			if !reflect.DeepEqual(namesBefore, namesAfter) {
				t.Fatalf("directory changed: %v -> %v", namesBefore, namesAfter)
			}
		})
	}
}
