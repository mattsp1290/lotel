package collector

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func StartBackend(ctx context.Context, configPath, dataPath, backend string) error {
	if backend != "docker" && backend != "native" {
		return fmt.Errorf("unknown backend %q; use docker or native", backend)
	}
	unlock, err := lockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	if backend == "native" {
		return startNative(ctx, configPath, dataPath)
	}
	state, err := readState()
	if err != nil {
		return err
	}
	if state != nil && state.Backend == "native" {
		status, statusErr := nativeStatus(ctx, state)
		if statusErr != nil {
			return statusErr
		}
		if status.Running {
			return errors.New("native collector is running; run 'lotel stop' before switching backend")
		}
		if err := stopNative(ctx, state); err != nil {
			return err
		}
	}
	return Start(ctx, configPath, dataPath)
}

func StopManaged(ctx context.Context) error {
	unlock, err := lockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	return Stop(ctx)
}

func lockLifecycle() (func(), error) {
	path, err := stateFilePath()
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(filepath.Dir(path), "lifecycle.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(file.Fd()), syscall.LOCK_UN); file.Close() }, nil
}

func startNative(ctx context.Context, configPath, dataPath string) error {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	dataPath, err = filepath.Abs(dataPath)
	if err != nil {
		return err
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(configBytes))
	state, err := readState()
	if err != nil {
		return err
	}
	if state != nil && state.Backend == "native" {
		status, statusErr := nativeStatus(ctx, state)
		if statusErr != nil {
			return statusErr
		}
		if status.Running {
			if state.ConfigPath != configPath {
				return fmt.Errorf("collector is running with config %s; run 'lotel stop' before switching config", state.ConfigPath)
			}
			if state.ConfigSHA256 == hash && state.DataPath == dataPath {
				fmt.Printf("Collector is already running (PID %d).\n", state.PID)
				return nil
			}
		}
		if err := stopNative(ctx, state); err != nil {
			return err
		}
	}
	if isContainerRunning() {
		return errors.New("Docker collector is running; run 'lotel stop' before switching backend")
	}
	for _, signal := range []string{"traces", "metrics", "logs", "profiles"} {
		if err := os.MkdirAll(filepath.Join(dataPath, signal), 0o755); err != nil {
			return err
		}
	}
	controlDir, err := os.MkdirTemp("", "lotel-")
	if err != nil {
		return err
	}
	keepControl := false
	defer func() {
		if !keepControl {
			os.RemoveAll(controlDir)
		}
	}()
	socket := filepath.Join(controlDir, "collector.sock")
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(entropy[:])
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	statePath, err := stateFilePath()
	if err != nil {
		return err
	}
	logPath := filepath.Join(filepath.Dir(statePath), "collector.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(executable, "run-collector", "--config", configPath, "--data", dataPath, "--control", socket, "--token", token)
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	state = &State{Backend: "native", PID: command.Process.Pid, ControlSocket: socket, ControlToken: token, StartedAt: time.Now(), ConfigPath: configPath, ConfigSHA256: hash, DataPath: dataPath}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		status, statusErr := nativeStatus(ctx, state)
		if statusErr == nil && status.Healthy {
			if err := writeState(state); err != nil {
				_, _ = nativeRequest(ctx, state, http.MethodPost, "/shutdown")
				<-done
				return err
			}
			keepControl = true
			fmt.Printf("Collector started (native PID %d).\nConfig: %s\nData: %s\nLog: %s\n", state.PID, configPath, dataPath, logPath)
			return nil
		}
		select {
		case processErr := <-done:
			return fmt.Errorf("native collector exited during startup (%v); inspect %s", processErr, logPath)
		case <-ctx.Done():
			_, _ = nativeRequest(context.Background(), state, http.MethodPost, "/shutdown")
			command.Process.Signal(syscall.SIGTERM)
			<-done
			return ctx.Err()
		case <-deadline.C:
			_, _ = nativeRequest(context.Background(), state, http.MethodPost, "/shutdown")
			command.Process.Signal(syscall.SIGTERM)
			<-done
			return fmt.Errorf("native collector did not become ready; inspect %s", logPath)
		case <-tick.C:
		}
	}
}

var errNativeUnavailable = errors.New("native collector control is unavailable")

func nativeRequest(ctx context.Context, state *State, method, path string) (*Status, error) {
	if state.ControlSocket == "" || state.ControlToken == "" {
		return nil, errNativeUnavailable
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", state.ControlSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, method, "http://lotel"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+state.ControlToken)
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, errNativeUnavailable
		}
		return nil, err
	}
	defer response.Body.Close()
	if path == "/shutdown" && response.StatusCode == http.StatusAccepted {
		return &Status{Backend: "native", Running: true}, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("native collector control returned HTTP %d", response.StatusCode)
	}
	var status Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func nativeStatus(ctx context.Context, state *State) (*Status, error) {
	status, err := nativeRequest(ctx, state, http.MethodGet, "/status")
	if errors.Is(err, errNativeUnavailable) {
		return &Status{Backend: "native"}, nil
	}
	if err == nil && status.Running {
		status.Uptime = time.Since(state.StartedAt).Truncate(time.Second).String()
	}
	return status, err
}

func stopNative(ctx context.Context, state *State) error {
	if _, err := nativeRequest(ctx, state, http.MethodPost, "/shutdown"); err != nil && !errors.Is(err, errNativeUnavailable) {
		return err
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		_, err := os.Stat(state.ControlSocket)
		if state.ControlSocket == "" || errors.Is(err, os.ErrNotExist) {
			break
		}
		if _, controlErr := nativeRequest(ctx, state, http.MethodGet, "/status"); errors.Is(controlErr, errNativeUnavailable) {
			os.Remove(state.ControlSocket)
			os.Remove(filepath.Dir(state.ControlSocket))
			break
		} else if controlErr != nil {
			return controlErr
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("native collector did not finish shutdown; state retained")
		case <-tick.C:
		}
	}
	if err := removeState(); err != nil {
		return err
	}
	fmt.Println("Collector stopped.")
	return nil
}
