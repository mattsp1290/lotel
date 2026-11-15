//go:build !windows

package collector_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInterruptedDockerStartup(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "docker-run-started")
	docker := `#!/bin/sh
case "$1" in
  inspect) exit 1 ;;
  ps) exit 0 ;;
  run)
    printf ready > "$LOTEL_DOCKER_TEST_READY"
    /bin/sleep 1
    printf 'fake-container\n'
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := f.command(ctx, "start")
	cmd.Env = append(f.env, "PATH="+binDir, "LOTEL_DOCKER_TEST_READY="+marker)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("Docker startup did not reach its pending run command")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var exitErr *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exitErr) {
		t.Fatalf("Docker startup did not terminate after SIGTERM: %v\n%s", err, output.String())
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("Docker startup exited without the requested SIGTERM: %v\n%s", exitErr, output.String())
	}
}
