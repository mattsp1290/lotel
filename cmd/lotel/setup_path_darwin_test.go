package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSetupMaximumLengthDirectoryPath(t *testing.T) {
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for len(path) < 1023 {
		remaining := 1023 - len(path)
		size := min(200, remaining-1)
		if remaining-size-1 == 1 {
			size--
		}
		if size < 1 {
			t.Fatalf("cannot construct 1023-byte path from %d bytes", len(path))
		}
		path += "/" + strings.Repeat("d", size)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	file, err := unix.Openat(fd, "go.mod", unix.O_CREAT|unix.O_WRONLY|unix.O_CLOEXEC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(file); err != nil {
		t.Fatal(err)
	}
	defer unix.Unlinkat(fd, "go.mod", 0)
	report, _, err := runSetup(t, "--recommend-only", "--path", path)
	if err != nil || len(report.Candidates) != 1 || report.Candidates[0].Ecosystem != "Go" {
		t.Fatalf("maximum path: %+v %v", report, err)
	}
}
