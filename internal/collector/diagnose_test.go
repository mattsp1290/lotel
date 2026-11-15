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
	"sync/atomic"
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
	state, err := json.Marshal(State{Backend: "native", PID: 1234, ControlSocket: socket, ControlToken: secret})
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
		{"explicit null error", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05Z","last_error":null}}`, 0, false, false, false, false, false},
		{"explicit null success", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":null}}`, 0, false, false, false, false, false},
		{"healthy but stopped", `{"backend":"native","running":false,"healthy":true,"ingestion":{"enabled":false,"running":false,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"healthy with no success", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"healthy with stopped ingestion", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":false,"interval":"2s","last_success":"2025-01-02T03:04:05Z"}}`, 0, false, false, false, false, false},
		{"healthy with ingestion error", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05Z","last_error":"diagnosis-secret-token"}}`, 0, false, false, false, false, false},
		{"native false healthy despite ready ingestion", `{"backend":"native","running":true,"healthy":false,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05Z"}}`, 0, false, false, false, false, false},
		{"missing pid", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":false,"running":false,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"mismatched pid", `{"backend":"native","pid":4321,"running":true,"healthy":true,"ingestion":{"enabled":false,"running":false,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"invalid offset", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05+24:00"}}`, 0, false, false, false, false, false},
		{"invalid minute", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05+00:60"}}`, 0, false, false, false, false, false},
		{"excess precision", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05.1234567891Z"}}`, 0, false, false, false, false, false},
		{"utc year underflow", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"0000-01-01T00:00:00+01:00"}}`, 0, false, false, false, false, false},
		{"valid offset", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05.123456789+02:30"}}`, 0, true, true, true, true, false},
		{"disabled but running", `{"backend":"native","running":true,"healthy":false,"ingestion":{"enabled":false,"running":true,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"invalid zero interval", `{"backend":"native","running":true,"healthy":false,"ingestion":{"enabled":true,"running":false,"interval":"0s"}}`, 0, false, false, false, false, false},
		{"wrong backend", `{"backend":"docker","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"2s"}}`, 0, false, false, false, false, false},
		{"bad interval", `{"backend":"native","running":true,"healthy":true,"ingestion":{"enabled":true,"running":true,"interval":"diagnosis-secret-token"}}`, 0, false, false, false, false, false},
		{"unauthorized", `diagnosis-secret-token`, http.StatusUnauthorized, false, false, false, false, false},
		{"malformed", `{"running":`, 0, false, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, code = tc.body, tc.code
			if tc.name != "missing pid" && tc.name != "mismatched pid" {
				body = strings.Replace(body, `"backend":"native"`, `"backend":"native","pid":1234`, 1)
			}
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
	state, err = json.Marshal(State{Backend: "docker", PID: 1234, ControlSocket: socket, ControlToken: secret})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stateFile), state, 0600); err != nil {
		t.Fatal(err)
	}
	body, code = `{"backend":"docker","pid":1234,"running":true,"healthy":false,"ingestion":{"enabled":true,"running":true,"interval":"2s","last_success":"2025-01-02T03:04:05Z"}}`, 0
	got := DiagnoseRuntime(context.Background())
	if got.Runtime.Observation != "available" || got.Runtime.CombinedHealthy == nil || *got.Runtime.CombinedHealthy {
		t.Fatalf("Docker health failure must remain observable: %+v", got)
	}
	server.Close()
	got = DiagnoseRuntime(context.Background())
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
	data, _ := json.Marshal(State{Backend: "native", PID: 1234, ControlSocket: socket, ControlToken: "token"})
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

func TestDiagnoseRuntimeRequiresPIDAndRejectsRedirect(t *testing.T) {
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
	const secret = "runtime-auth-secret"
	var requests, redirected atomic.Int32
	var redirect atomic.Bool
	var incorrectAuth atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+secret {
			incorrectAuth.Store(true)
		}
		if r.URL.Path != "/status" {
			redirected.Add(1)
		}
		if redirect.Load() && r.URL.Path == "/status" {
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"backend":"native","pid":1234,"running":true,"healthy":true,"ingestion":{"enabled":false,"running":false,"interval":"2s"}}`))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	path := filepath.Join(dir, stateFile)
	for _, tc := range []struct {
		name    string
		pid     any
		include bool
	}{
		{"missing", nil, false},
		{"null", nil, true},
		{"zero", 0, true},
		{"negative", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := map[string]any{"backend": "native", "control_socket": socket, "control_token": secret}
			if tc.include {
				state["pid"] = tc.pid
			}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			before := requests.Load()
			got := DiagnoseRuntime(context.Background())
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if got.StateFile.Observation != "present" || got.Runtime.Observation != "unknown" || got.Runtime.Reason != "state_invalid" || got.Ingestion.Observation != "unknown" || requests.Load() != before {
				t.Fatalf("invalid PID was accepted: %s", encoded)
			}
			if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), socket) {
				t.Fatalf("leaked runtime state: %s", encoded)
			}
		})
	}
	state, err := json.Marshal(State{Backend: "native", PID: 1234, ControlSocket: socket, ControlToken: secret})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, state, 0600); err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	got := DiagnoseRuntime(context.Background())
	if got.Runtime.Observation != "available" || got.Ingestion.Observation != "available" || requests.Load() != before+1 {
		t.Fatalf("valid runtime was not observed: %+v", got)
	}
	redirect.Store(true)
	got = DiagnoseRuntime(context.Background())
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Runtime.Observation != "unavailable" || got.Runtime.Reason != "runtime_unavailable" || got.Ingestion.Observation != "unknown" || requests.Load() != before+2 || redirected.Load() != 0 || incorrectAuth.Load() {
		t.Fatalf("redirect was followed or not reported: %s", encoded)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), socket) {
		t.Fatalf("redirect diagnosis leaked runtime state: %s", encoded)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(state) {
		t.Fatalf("diagnosis modified state: %v", err)
	}
}
