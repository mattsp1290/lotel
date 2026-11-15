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

func startRuntime(ctx context.Context, configPath, dataPath, backend string) error {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	dataPath, err = filepath.Abs(dataPath)
	if err != nil {
		return err
	}
	state, err := readState()
	if err != nil {
		return err
	}
	if state != nil && state.ControlSocket != "" {
		status, statusErr := requestRuntimeStatus(ctx, state)
		if statusErr != nil && !errors.Is(statusErr, errRuntimeUnavailable) {
			return statusErr
		}
		if statusErr == nil && status.Running {
			if state.Backend != backend {
				return fmt.Errorf("%s collector is running; run 'lotel stop' before switching backend", state.Backend)
			}
			if state.ConfigPath != configPath {
				if backend == "native" {
					return fmt.Errorf("collector is running with config %s; run 'lotel stop' before switching config", state.ConfigPath)
				}
				fmt.Fprintf(os.Stderr, "collector is running with config %s; run 'lotel stop' before switching config\n", state.ConfigPath)
				fmt.Printf("Collector is already running (PID %d).\n", state.PID)
				return nil
			}
			source, err := os.ReadFile(configPath)
			if err != nil {
				return err
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(source))
			if state.ConfigSHA256 == hash && state.DataPath == dataPath {
				fmt.Printf("Collector is already running (PID %d).\n", state.PID)
				return nil
			}
			if err := stopRuntime(ctx, state); err != nil {
				return err
			}
			state = nil
		} else if statusErr == nil {
			if stopErr := stopRuntime(ctx, state); stopErr != nil {
				remaining, readErr := readState()
				if readErr != nil || remaining != nil {
					return errors.Join(stopErr, readErr)
				}
				fmt.Fprintf(os.Stderr, "Previous Collector shutdown: %v\n", stopErr)
			}
			state = nil
		} else {
			os.Remove(state.ControlSocket)
			os.Remove(filepath.Dir(state.ControlSocket))
		}
	}
	createdDocker := false
	if backend == "docker" {
		before := state
		wasRunning := isContainerRunning()
		if err := startDocker(ctx, configPath, dataPath); err != nil {
			return err
		}
		state, err = readState()
		if err != nil {
			return err
		}
		if state == nil {
			return errors.New("Docker collector has no recorded config; restart it with lotel")
		}
		createdDocker = !wasRunning || before == nil || before.ContainerID != state.ContainerID
		configPath, dataPath = state.ConfigPath, state.DataPath
	} else {
		if isContainerRunning() {
			return errors.New("Docker collector is running; run 'lotel stop' before switching backend")
		}
		source, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		state = &State{StartedAt: time.Now(), ConfigPath: configPath, ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256(source)), DataPath: dataPath, ProfilesEnabled: true}
	}
	succeeded := false
	defer func() {
		if !succeeded && createdDocker {
			_ = stopDocker(context.Background())
			_ = removeState()
		}
	}()
	for _, signal := range []string{"traces", "metrics", "logs", "profiles"} {
		if err := os.MkdirAll(filepath.Join(dataPath, signal), 0o755); err != nil {
			return err
		}
	}
	controlDir, err := os.MkdirTemp("", "lotel-")
	if err != nil {
		return err
	}
	defer func() {
		if !succeeded {
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
	command := exec.Command(executable, "run-collector", "--backend", backend, "--config", configPath, "--data", dataPath, "--control", socket, "--token", token)
	command.Stdout, command.Stderr = log, log
	configureRuntimeProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	state.Backend, state.PID, state.ControlSocket, state.ControlToken = backend, command.Process.Pid, socket, token
	stopChild := func() {
		if backend == "docker" {
			_ = command.Process.Kill()
			<-done
		} else {
			stopStartupChild(command, done)
		}
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			stopChild()
			return err
		}
		status, statusErr := requestRuntimeStatus(ctx, state)
		if statusErr == nil && status.Running && (backend == "docker" || status.Healthy) {
			if err := ctx.Err(); err != nil {
				stopChild()
				return err
			}
			if err := writeState(state); err != nil {
				stopChild()
				return err
			}
			succeeded = true
			fmt.Printf("Collector started (%s runtime PID %d).\nConfig: %s\nData: %s\nLog: %s\n", backend, state.PID, configPath, dataPath, logPath)
			return nil
		}
		select {
		case processErr := <-done:
			return fmt.Errorf("collector runtime exited during startup (%v); inspect %s", processErr, logPath)
		case <-ctx.Done():
			stopChild()
			return ctx.Err()
		case <-deadline.C:
			stopChild()
			return fmt.Errorf("collector runtime did not become ready; inspect %s", logPath)
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
