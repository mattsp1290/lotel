//go:build !windows

package collector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeInspectionDocker(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestDockerInspectionAllowsSlowResponse(t *testing.T) {
	fakeInspectionDocker(t, "exec /bin/sh -c '/bin/sleep 1.2; printf true'\n")
	running, exists, err := inspectDockerState(t.Context())
	if err != nil || !running || !exists {
		t.Fatalf("slow running container = %t, %t, %v", running, exists, err)
	}
}

func TestDockerInspectionFallbackHasOwnDeadline(t *testing.T) {
	fakeInspectionDocker(t, "case \"$1\" in\ninspect) /bin/sleep 2.2; exit 1;;\nps) /bin/sleep 1.2; exit 0;;\n*) exit 1;;\nesac\n")
	running, exists, err := inspectDockerState(t.Context())
	if err != nil || running || exists {
		t.Fatalf("confirmed absent container = %t, %t, %v", running, exists, err)
	}
}

func TestDockerInspectionHonorsCancellation(t *testing.T) {
	fakeInspectionDocker(t, "exec /bin/sleep 30\n")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	running, exists, err := inspectDockerState(ctx)
	if running || exists || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled inspection = %t, %t, %v", running, exists, err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("inspection did not honor caller deadline promptly")
	}
}

func TestDockerInspectionFallbackHonorsCancellation(t *testing.T) {
	fakeInspectionDocker(t, "case \"$1\" in\ninspect) exit 1;;\nps) exec /bin/sleep 30;;\n*) exit 1;;\nesac\n")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	running, exists, err := inspectDockerState(ctx)
	if running || exists || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled fallback = %t, %t, %v", running, exists, err)
	}
}

func TestDockerInspectionDoesNotTreatFailedFallbackAsAbsent(t *testing.T) {
	fakeInspectionDocker(t, "exit 1\n")
	running, exists, err := inspectDockerState(t.Context())
	if running || exists || err == nil {
		t.Fatalf("failed inspection and inventory = %t, %t, %v", running, exists, err)
	}
}

func TestRuntimeStatusAllowsSlowDockerInspection(t *testing.T) {
	fakeInspectionDocker(t, "case \"$1\" in\ninspect) /bin/sleep 2.2; printf true;;\nstop|rm) exit 0;;\n*) exit 1;;\nesac\n")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	if err := os.WriteFile(configPath, []byte("service: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("/tmp", "lotel-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "runtime.sock")
	oldHealthURL := healthURL
	healthURL = "http://127.0.0.1:0/"
	t.Cleanup(func() { healthURL = oldHealthURL })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RuntimeOptions{Backend: "docker", ConfigPath: configPath, DataPath: dir, ControlSocket: socket, ControlToken: "test-token"})
	}()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime socket unavailable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := &State{Backend: "docker", PID: os.Getpid(), ControlSocket: socket, ControlToken: "test-token"}
	status, err := requestRuntimeStatus(t.Context(), state)
	if err != nil || status == nil || !status.Running {
		t.Fatalf("runtime status = %+v, %v", status, err)
	}
	t.Setenv("HOME", t.TempDir())
	if err := writeState(state); err != nil {
		t.Fatal(err)
	}
	diagnosis := DiagnoseRuntime(t.Context())
	if diagnosis.Runtime.Observation != "available" {
		t.Fatalf("slow runtime diagnosis = %+v", diagnosis)
	}
}

func TestHealthProbeHonorsCancellation(t *testing.T) {
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(finished)
	}))
	defer server.Close()
	oldHealthURL := healthURL
	healthURL = server.URL
	defer func() { healthURL = oldHealthURL }()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if checkHealthContext(ctx) {
		t.Fatal("canceled health probe reported healthy")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("canceled health probe did not cancel the request")
	}
}

func TestStatusHonorsCallerCancellation(t *testing.T) {
	for _, runtimeUnavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(runtimeUnavailable), func(t *testing.T) {
			fakeInspectionDocker(t, "exec /bin/sleep 30\n")
			t.Setenv("HOME", t.TempDir())
			if runtimeUnavailable {
				if err := writeState(&State{Backend: "docker", ControlSocket: "/nonexistent/socket", ControlToken: "test"}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err := GetStatus(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("status cancellation = %v", err)
			}
		})
	}
}

func TestWaitHealthyDeadlineIncludesProbe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	oldHealthURL := healthURL
	healthURL = server.URL
	defer func() { healthURL = oldHealthURL }()
	if err := WaitHealthy(t.Context(), 100*time.Millisecond); err == nil {
		t.Fatal("accepted health response after readiness deadline")
	}
}

func TestWaitHealthyDeadlineIncludesRuntimeStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "lotel-wait-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"running":true,"healthy":true}`))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	if err := writeState(&State{Backend: "docker", ControlSocket: socket, ControlToken: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := WaitHealthy(t.Context(), 100*time.Millisecond); err == nil {
		t.Fatal("accepted runtime status after readiness deadline")
	}
}
