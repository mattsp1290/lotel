package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func InspectForUpgrade(ctx context.Context) (*Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	state, err := readStateFile(filepath.Join(home, stateDir, stateFile))
	if err != nil {
		return nil, err
	}
	if state != nil && state.Backend == "native" {
		status, err := nativeStatus(ctx, state)
		if err != nil {
			return nil, err
		}
		if status.Running && status.ConfigPath == "" {
			return nil, errors.New("running Collector has no recorded config path; restart it with lotel before upgrading")
		}
		return status, nil
	}
	if state != nil && state.Backend != "" && state.Backend != "docker" {
		return nil, fmt.Errorf("inspect Collector: unknown recorded backend %q", state.Backend)
	}
	docker, err := exec.LookPath("docker")
	if errors.Is(err, exec.ErrNotFound) && state == nil {
		return &Status{Backend: "docker"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Collector: Docker is unavailable: %w", err)
	}
	return inspectDockerForUpgrade(ctx, docker, state)
}

func inspectDockerForUpgrade(ctx context.Context, docker string, state *State) (*Status, error) {
	list := exec.CommandContext(ctx, docker, "ps", "--all", "--filter", "name=^/"+containerName+"$", "--format", "{{.ID}}")
	list.WaitDelay = time.Second
	out, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Collector container list: %w", err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return &Status{Backend: "docker"}, nil
	}
	if len(ids) != 1 || len(ids[0]) < 12 || len(ids[0]) > 64 || strings.Trim(ids[0], "0123456789abcdef") != "" {
		return nil, errors.New("Docker returned an invalid Collector container ID")
	}
	inspect := exec.CommandContext(ctx, docker, "inspect", "--format", "{{.State.Running}}", ids[0])
	inspect.WaitDelay = time.Second
	out, err = inspect.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Collector running state: %w", err)
	}
	switch strings.TrimSpace(string(out)) {
	case "false":
		return &Status{Backend: "docker"}, nil
	case "true":
	default:
		return nil, errors.New("Docker returned an invalid Collector running state")
	}
	if state == nil || state.ConfigPath == "" {
		return nil, errors.New("running Collector has no recorded config path; restart it with lotel before upgrading")
	}
	return &Status{Running: true, Backend: "docker", ConfigPath: state.ConfigPath}, nil
}
