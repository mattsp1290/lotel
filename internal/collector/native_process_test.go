package collector

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStopNativeCleansCrashedSocketWithoutSignalingPID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("", "lotel-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "control.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	listener.Close()
	state := &State{Backend: "native", PID: os.Getpid(), ControlSocket: path, ControlToken: "stale"}
	if err := writeState(state); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if state, err := readState(); err != nil || state != nil {
		t.Fatalf("stale state remains: %+v %v", state, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale socket remains: %v", err)
	}
}

func TestStopNativeRetainsStateWhenOwnershipCannotBeVerified(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("", "lotel-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })}
	go server.Serve(listener)
	defer server.Close()
	state := &State{Backend: "native", PID: os.Getpid(), ControlSocket: path, ControlToken: "wrong"}
	if err := writeState(state); err != nil {
		t.Fatal(err)
	}
	if err := Stop(context.Background()); err == nil {
		t.Fatal("stopped an unverified collector")
	}
	if state, err := readState(); err != nil || state == nil {
		t.Fatalf("unverified state was removed: %+v %v", state, err)
	}
}
