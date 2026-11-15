package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func Start(ctx context.Context, configPath, dataPath, backend string) error {
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
	return startDocker(ctx, configPath, dataPath)
}

func Stop(ctx context.Context) error {
	unlock, err := lockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := readState()
	if err != nil {
		return err
	}
	if state != nil && state.Backend == "native" {
		return stopNative(ctx, state)
	}
	return stopDocker(ctx)
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
