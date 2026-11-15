package collector

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleMutationWaitsForLock(t *testing.T) {
	if action := os.Getenv("LOTEL_LIFECYCLE_TEST_ACTION"); action != "" {
		if err := os.WriteFile(os.Getenv("LOTEL_LIFECYCLE_TEST_READY"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if action == "stop" {
			if err := Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
		} else if err := Start(context.Background(), os.Getenv("LOTEL_LIFECYCLE_TEST_CONFIG"), os.Getenv("LOTEL_LIFECYCLE_TEST_DATA"), "docker"); err == nil {
			t.Fatal("Docker start succeeded without an executable")
		}
		return
	}
	for _, action := range []string{"start", "stop"} {
		t.Run(action, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("PATH", t.TempDir())
			unlock, err := lockLifecycle()
			if err != nil {
				t.Fatal(err)
			}
			locked := true
			defer func() {
				if locked {
					unlock()
				}
			}()
			if err := writeState(&State{ContainerID: "must-survive-while-locked"}); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(home, "collector.yaml")
			if err := os.WriteFile(config, []byte("receivers: {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ready := filepath.Join(home, "mutation-ready")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLifecycleMutationWaitsForLock$")
			cmd.Env = append(os.Environ(), "LOTEL_LIFECYCLE_TEST_ACTION="+action, "LOTEL_LIFECYCLE_TEST_READY="+ready, "LOTEL_LIFECYCLE_TEST_CONFIG="+config, "LOTEL_LIFECYCLE_TEST_DATA="+filepath.Join(home, "data"))
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			waited := false
			defer func() {
				if !waited {
					cancel()
					<-done
				}
			}()
			deadline := time.Now().Add(2 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mutation subprocess did not become ready")
				}
				time.Sleep(5 * time.Millisecond)
			}
			select {
			case err := <-done:
				waited = true
				t.Fatalf("%s completed before the lifecycle lock was released: %v\n%s", action, err, output.String())
			case <-time.After(150 * time.Millisecond):
			}
			state, err := readState()
			if err != nil || state == nil || state.ContainerID != "must-survive-while-locked" {
				t.Fatalf("%s changed state while lifecycle lock was held: %+v, %v", action, state, err)
			}
			unlock()
			locked = false
			err = <-done
			waited = true
			if err != nil {
				t.Fatalf("%s did not finish after lock release: %v\n%s", action, err, output.String())
			}
			if state, err := readState(); err != nil || state != nil {
				t.Fatalf("%s did not complete its state mutation after lock release: %+v, %v", action, state, err)
			}
		})
	}
}
