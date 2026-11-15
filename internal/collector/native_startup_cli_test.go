//go:build !windows

package collector_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInterruptedNativeStartup(t *testing.T) {
	processors := "  transform:\n    log_statements:\n" + strings.Repeat("      - set(log.attributes[\"review.attr\"], \"value\")\n", 10000)
	f := newNativeCLIFixture(t, processors)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := f.command(ctx, "start", "--backend", "native", "--wait")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	child := 0
	deadline := time.Now().Add(5 * time.Second)
	for child == 0 && time.Now().Before(deadline) {
		ps, err := exec.Command("/bin/ps", "-Ao", "pid=,ppid=,args=").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(ps), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 3 && fields[1] == strconv.Itoa(cmd.Process.Pid) && strings.Contains(line, " run-collector ") {
				child, err = strconv.Atoi(fields[0])
				if err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		if child == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if child == 0 {
		t.Fatal("did not observe the owned native startup child")
	}
	cleanupChild := child
	t.Cleanup(func() {
		if cleanupChild != 0 && syscall.Kill(cleanupChild, 0) == nil {
			_ = syscall.Kill(cleanupChild, syscall.SIGKILL)
		}
	})
	if _, err := os.Stat(filepath.Join(f.home, ".lotel", "collector.state")); !os.IsNotExist(err) {
		t.Fatalf("startup interruption window closed: %v", err)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	waited = true
	if err == nil {
		t.Fatalf("interrupted start reported success: %s", output.String())
	}
	if err := syscall.Kill(child, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("interrupted start did not reap child %d: %v\n%s", child, err, output.String())
	}
	cleanupChild = 0
	if out, err := f.run("status"); err == nil {
		t.Fatalf("interrupted collector reported running: %s", out)
	}
}
