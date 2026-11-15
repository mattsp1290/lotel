package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var errRuntimeUnavailable = errors.New("collector runtime control is unavailable")

func runtimeControlRequest(ctx context.Context, state *State, method, path string, timeout time.Duration, checkRedirect func(*http.Request, []*http.Request) error) (*http.Response, error) {
	if state.ControlSocket == "" || state.ControlToken == "" {
		return nil, errRuntimeUnavailable
	}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", state.ControlSocket)
		},
	}
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: checkRedirect}
	req, err := http.NewRequestWithContext(ctx, method, "http://lotel"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+state.ControlToken)
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, errRuntimeUnavailable
		}
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("collector runtime control returned HTTP %d", response.StatusCode)
	}
	return response, nil
}

func requestRuntimeStatus(ctx context.Context, state *State) (*Status, error) {
	response, err := runtimeControlRequest(ctx, state, http.MethodGet, "/status", 2*time.Second, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var status Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func requestRuntimeShutdown(ctx context.Context, state *State) (runtimeShutdownResult, error) {
	var result runtimeShutdownResult
	response, err := runtimeControlRequest(ctx, state, http.MethodPost, "/shutdown", 45*time.Second, nil)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}

func runtimeStatus(ctx context.Context, state *State) (*Status, error) {
	status, err := requestRuntimeStatus(ctx, state)
	if errors.Is(err, errRuntimeUnavailable) {
		status := &Status{Backend: state.Backend, ConfigPath: state.ConfigPath}
		status.Ingestion.Enabled = true
		status.Ingestion.LastError = "collector runtime is unavailable"
		if state.Backend == "docker" {
			status.Running = isContainerRunning()
			status.ContainerID = state.ContainerID
			status.ContainerName = state.ContainerName
		}
		return status, nil
	}
	if err == nil && status.Running {
		status.Uptime = time.Since(state.StartedAt).Truncate(time.Second).String()
		status.ConfigPath = state.ConfigPath
		status.ContainerID = state.ContainerID
		status.ContainerName = state.ContainerName
		status.Image = state.Image
	}
	return status, err
}

func stopRuntime(ctx context.Context, state *State) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := requestRuntimeShutdown(ctx, state)
	if err != nil && !errors.Is(err, errRuntimeUnavailable) {
		return err
	}
	if errors.Is(err, errRuntimeUnavailable) {
		if state.Backend == "docker" {
			if err := finishUnmanagedDocker(ctx, state); err != nil {
				return err
			}
		}
		if state.ControlSocket != "" {
			os.Remove(state.ControlSocket)
			os.Remove(filepath.Dir(state.ControlSocket))
		}
	} else if err := waitRuntimeStopped(ctx, state); err != nil {
		return err
	}
	if state.Backend == "docker" && result.Error != "" {
		running, inspectErr := inspectDockerRunning(ctx)
		if inspectErr != nil || running {
			return errors.Join(fmt.Errorf("Collector shutdown failed; state retained: %s", result.Error), inspectErr)
		}
	}
	if err := removeState(); err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("collector stopped with error: %s", result.Error)
	}
	fmt.Println("Collector stopped.")
	return nil
}

func waitRuntimeStopped(ctx context.Context, state *State) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		_, err := os.Stat(state.ControlSocket)
		if state.ControlSocket == "" || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("collector runtime did not finish shutdown; state retained: %w", ctx.Err())
		case <-tick.C:
		}
	}
}
