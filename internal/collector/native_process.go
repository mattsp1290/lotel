package collector

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	if backend == "native" && !nativeSupported() {
		return errors.New("native collector is supported only on macOS and Linux; use --backend docker")
	}
	unlock, err := lockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if err := lockLifecycleFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return func() { unlockLifecycleFile(file); file.Close() }, nil
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
	configureNativeProcess(command)
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
		if err := ctx.Err(); err != nil {
			stopStartupChild(command, done)
			return err
		}
		status, statusErr := nativeStatus(ctx, state)
		if statusErr == nil && status.Healthy {
			if err := ctx.Err(); err != nil {
				stopStartupChild(command, done)
				return err
			}
			if err := writeState(state); err != nil {
				stopStartupChild(command, done)
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
			stopStartupChild(command, done)
			return ctx.Err()
		case <-deadline.C:
			stopStartupChild(command, done)
			return fmt.Errorf("native collector did not become ready; inspect %s", logPath)
		case <-tick.C:
		}
	}
}

func stopStartupChild(command *exec.Cmd, done <-chan error) {
	_ = command.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}
