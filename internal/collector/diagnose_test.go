package collector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiagnoseRuntimeMissingAndInvalidState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := DiagnoseRuntime(context.Background())
	if got.StateFile.Observation != "missing" || got.Runtime.Observation != "unknown" || got.Runtime.Running != nil || got.Ingestion.Observation != "unknown" {
		t.Fatalf("missing state: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, stateDir)); !os.IsNotExist(err) {
		t.Fatalf("diagnosis created state directory: %v", err)
	}
	dir := filepath.Join(home, stateDir)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, stateFile)
	if err := os.WriteFile(path, []byte(`{"backend":`), 0600); err != nil {
		t.Fatal(err)
	}
	got = DiagnoseRuntime(context.Background())
	if got.StateFile.Observation != "unavailable" || got.StateFile.Reason != "state_invalid" || got.Runtime.Observation != "unknown" {
		t.Fatalf("invalid state: %+v", got)
	}
	if err := os.WriteFile(path, []byte(`{"backend":"native"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got = DiagnoseRuntime(context.Background())
	if got.StateFile.Observation != "present" || got.Runtime.Reason != "state_invalid" || got.Ingestion.Observation != "unknown" {
		t.Fatalf("incomplete state: %+v", got)
	}
}

func TestDiagnoseRuntimeObservations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix runtime socket")
	}
	home, err := os.MkdirTemp("", "lotel-diag-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	dir := filepath.Join(home, stateDir)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(home, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	secret := "diagnosis-secret-token"
	var body string
	var code int
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/status" || r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if code != 0 {
			w.WriteHeader(code)
		}
		_, _ = w.Write([]byte(body))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	state, err := json.Marshal(State{Backend: "native", ControlSocket: socket, ControlToken: secret})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stateFile), state, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body                          string
		code                                int
		available                           bool
		enabled, running, healthy, hasError bool
	}{
		{"healthy", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05Z"}}`, 0, true, true, true, true, false},
		{"disabled", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":false,"running":false,"interval":"2s"}}`, 0, true, false, false, true, false},
		{"stopped with error", `{"backend":"native","running":false,"healthy":false,"ingestion":{"enabled":true,"running":false,"interval":"2s","last_error":"diagnosis-secret-token"}}`, 0, true, true, false, false, true},
		{"empty response", `{}`, 0, false, false, false, false, false},
		{"partial ingestion", `{"backend":"native","running":false,"healthy":false,"ingestion":{"enabled":false}}`, 0, false, false, false, false, false},
		{"explicit null error", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_error":null}}`, 0, false, false, false, false, false},
		{"explicit null success", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":null}}`, 0, false, false, false, false, false},
		{"healthy but stopped", `{"backend":"native","running":false,"healthy":true,"ingestion":{"enabled":false,"running":false,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"healthy with no success", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"healthy with stopped ingestion", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":false,"interval":"2s","last_success":"2025-01-02T03:04:05Z"}}`, 0, false, false, false, false, false},
		{"healthy with ingestion error", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05Z","last_error":"diagnosis-secret-token"}}`, 0, false, false, false, false, false},
		{"disabled but running", `{"backend":"native","running":true,"healthy":false,"ingestion":{"enabled":false,"running":true,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"invalid zero interval", `{"backend":"native","running":true,"healthy":false,"ingestion":{"enabled":true,"running":false,"interval":"0s"}}`, 0, false, false, false, false, false},
		{"wrong backend", `{"backend":"docker","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"bad interval", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"diagnosis-secret-token"}}`, 0, false, false, false, false, false},
		{"unauthorized", `diagnosis-secret-token`, http.StatusUnauthorized, false, false, false, false, false},
		{"malformed", `{"running":`, 0, false, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, code = tc.body, tc.code
			got := DiagnoseRuntime(context.Background())
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), socket) {
				t.Fatalf("leaked secret: %s", encoded)
			}
			if tc.available {
				if got.Runtime.Observation != "available" || got.Ingestion.Observation != "available" || got.Runtime.Running == nil || *got.Runtime.Running != (tc.name != "stopped with error") || got.Runtime.CombinedHealthy == nil || *got.Runtime.CombinedHealthy != tc.healthy || got.Ingestion.Enabled == nil || *got.Ingestion.Enabled != tc.enabled || got.Ingestion.Running == nil || *got.Ingestion.Running != tc.running || got.Ingestion.HasError == nil || *got.Ingestion.HasError != tc.hasError {
					t.Fatalf("unexpected report: %s", encoded)
				}
			} else if got.Runtime.Observation != "unavailable" || got.Runtime.Running != nil || got.Ingestion.Observation != "unknown" || got.Ingestion.HasError != nil {
				t.Fatalf("invalid response was treated as observed: %s", encoded)
			}
		})
	}
	server.Close()
	got := DiagnoseRuntime(context.Background())
	if got.Runtime.Observation != "unavailable" || got.Ingestion.Observation != "unknown" {
		t.Fatalf("unavailable socket: %+v", got)
	}
}

func TestDiagnoseRuntimeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix runtime socket")
	}
	home, err := os.MkdirTemp("", "lotel-diag-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, stateDir), 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(home, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	data, _ := json.Marshal(State{Backend: "native", ControlSocket: socket, ControlToken: "token"})
	if err := os.WriteFile(filepath.Join(home, stateDir, stateFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := DiagnoseRuntime(ctx)
	if got.Runtime.Observation != "unavailable" || time.Since(start) > time.Second {
		t.Fatalf("timeout: %+v", got)
	}
}
