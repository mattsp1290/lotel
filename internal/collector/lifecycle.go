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
	return startRuntime(ctx, configPath, dataPath, backend)
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
	if state != nil && state.ControlSocket != "" {
		return stopRuntime(ctx, state)
	}
	return errors.Join(stopDocker(ctx), removeState())
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
