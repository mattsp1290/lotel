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

var errNativeUnavailable = errors.New("native collector control is unavailable")

func nativeControlRequest(ctx context.Context, state *State, method, path string, timeout time.Duration) (*http.Response, error) {
	if state.ControlSocket == "" || state.ControlToken == "" {
		return nil, errNativeUnavailable
	}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", state.ControlSocket)
		},
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
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
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("native collector control returned HTTP %d", response.StatusCode)
	}
	return response, nil
}

func requestNativeStatus(ctx context.Context, state *State) (*Status, error) {
	response, err := nativeControlRequest(ctx, state, http.MethodGet, "/status", 2*time.Second)
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

func requestNativeShutdown(ctx context.Context, state *State) (nativeShutdownResult, error) {
	var result nativeShutdownResult
	response, err := nativeControlRequest(ctx, state, http.MethodPost, "/shutdown", 30*time.Second)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}

func nativeStatus(ctx context.Context, state *State) (*Status, error) {
	status, err := requestNativeStatus(ctx, state)
	if errors.Is(err, errNativeUnavailable) {
		return &Status{Backend: "native", ConfigPath: state.ConfigPath}, nil
	}
	if err == nil {
		status.ConfigPath = state.ConfigPath
		if status.Running {
			status.Uptime = time.Since(state.StartedAt).Truncate(time.Second).String()
		}
	}
	return status, err
}

func stopNative(ctx context.Context, state *State) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := requestNativeShutdown(ctx, state)
	if err != nil && !errors.Is(err, errNativeUnavailable) {
		return err
	}
	if errors.Is(err, errNativeUnavailable) {
		if state.ControlSocket != "" {
			os.Remove(state.ControlSocket)
			os.Remove(filepath.Dir(state.ControlSocket))
		}
	} else if err := waitNativeStopped(ctx, state); err != nil {
		return err
	}
	if err := removeState(); err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("native collector stopped with error: %s", result.Error)
	}
	fmt.Println("Collector stopped.")
	return nil
}

func waitNativeStopped(ctx context.Context, state *State) error {
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
			return fmt.Errorf("native collector did not finish shutdown; state retained: %w", ctx.Err())
		case <-tick.C:
		}
	}
}
