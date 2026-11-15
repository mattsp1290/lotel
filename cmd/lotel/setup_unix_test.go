//go:build darwin || linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSetupSkipsFIFO(t *testing.T) {
	project := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(project, "go.mod"), 0600); err != nil {
		t.Fatal(err)
	}
	report, _, err := runSetup(t, "--recommend-only", "--path", project)
	if err != nil || len(report.Candidates) != 0 || len(report.Skipped) != 1 || report.Skipped[0] != (setupSkipped{Manifest: "go.mod", Reason: "not_regular"}) {
		t.Fatalf("FIFO report: %+v %v", report, err)
	}
}

func TestSetupResolvesSymlinkBeforeParent(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "go.mod"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "pyproject.toml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(target, "child"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	rawPath := base + string(os.PathSeparator) + "link" + string(os.PathSeparator) + ".."
	report, _, err := runSetup(t, "--recommend-only", "--path", rawPath)
	if err != nil || len(report.Candidates) != 1 || report.Candidates[0].Ecosystem != "Python" {
		t.Fatalf("symlink parent: %+v %v", report, err)
	}
}

func TestSetupPinsSelectedDirectory(t *testing.T) {
	base := t.TempDir()
	first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, "go.mod"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "package.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				finished <- nil
				return
			default:
			}
			target := first
			if i%2 == 1 {
				target = second
			}
			temp := filepath.Join(base, "replacement")
			if err := os.Symlink(target, temp); err != nil {
				finished <- err
				return
			}
			if err := os.Rename(temp, link); err != nil {
				finished <- err
				return
			}
		}
	}()
	successes := 0
	for i := 0; i < 300; i++ {
		report, output, err := runSetup(t, "--recommend-only", "--path", link)
		if err != nil && len(output) == 0 {
			continue
		}
		if err != nil || len(report.Candidates) != 1 || (report.Candidates[0].Ecosystem != "Go" && report.Candidates[0].Ecosystem != "JavaScript") {
			close(stop)
			<-finished
			t.Fatalf("mixed directory result: %+v %v", report, err)
		}
		successes++
	}
	close(stop)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if successes == 0 {
		t.Fatal("no directory was inspected")
	}
}

func TestSetupRejectsFIFOProjectPathPromptly(t *testing.T) {
	project := t.TempDir()
	fifo := filepath.Join(project, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIHelper$", "--", "setup", "--recommend-only", "--path", fifo)
	cmd.Env = append(os.Environ(), "LOTEL_CLI_HELPER=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err == nil || !strings.Contains(string(output), "inspect project directory") {
		t.Fatalf("FIFO result: %v %v %s", err, ctx.Err(), output)
	}
}
