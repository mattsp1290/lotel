package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mattsp1290/lotel/internal/config"
	"github.com/mattsp1290/lotel/internal/ingestion"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// State represents the persisted state of a managed collector.
type State struct {
	ContainerID     string    `json:"container_id"`
	ContainerName   string    `json:"container_name"`
	Image           string    `json:"image"`
	StartedAt       time.Time `json:"started_at"`
	ConfigSHA256    string    `json:"config_sha256"`
	ConfigPath      string    `json:"config_path"`
	DataPath        string    `json:"data_path"`
	ProfilesEnabled bool      `json:"profiles_enabled"`
	Backend         string    `json:"backend,omitempty"`
	PID             int       `json:"pid,omitempty"`
	ControlSocket   string    `json:"control_socket,omitempty"`
	ControlToken    string    `json:"control_token,omitempty"`
}

// Status represents the current status of the collector.
type Status struct {
	Ingestion     ingestion.Status `json:"ingestion"`
	ConfigPath    string           `json:"config_path,omitempty"`
	Backend       string           `json:"backend,omitempty"`
	PID           int              `json:"pid,omitempty"`
	Running       bool             `json:"running"`
	ContainerID   string           `json:"container_id,omitempty"`
	ContainerName string           `json:"container_name,omitempty"`
	Healthy       bool             `json:"healthy"`
	Uptime        string           `json:"uptime,omitempty"`
	Image         string           `json:"image,omitempty"`
}

const (
	stateDir  = ".lotel"
	stateFile = "collector.state"

	containerName = "lotel-collector"
	defaultImage  = "otel/opentelemetry-collector-contrib:0.165.0"
)

var healthURL = "http://localhost:13133/"

func stateFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	dir := filepath.Join(home, stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating state directory: %w", err)
	}
	return filepath.Join(dir, stateFile), nil
}

func readState() (*State, error) {
	path, err := stateFilePath()
	if err != nil {
		return nil, err
	}
	return readStateFile(path)
}

func readStateFile(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}
	return &s, nil
}

func writeState(s *State) error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	// Atomic write: write to temp file then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}
	return os.Rename(tmp, path)
}

func removeState() error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing state file: %w", err)
	}
	return nil
}

// findDocker locates the docker binary.
func findDocker() (string, error) {
	path, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("docker not found in PATH; install from https://docs.docker.com/get-docker/")
	}
	return path, nil
}

// isContainerRunning checks if the lotel-collector container is running.
func isContainerRunning() bool {
	running, _ := inspectDockerRunning(context.Background())
	return running
}

func inspectDockerRunning(ctx context.Context) (bool, error) {
	running, _, err := inspectDockerState(ctx)
	return running, err
}

// inspectDockerState distinguishes absence from stopped and unknown Docker state.
func inspectDockerState(ctx context.Context) (running, exists bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		// A successful inventory can confirm removal; a failed inspect alone cannot.
		list := exec.CommandContext(ctx, "docker", "ps", "--all", "--filter", "name=^/"+containerName+"$", "--format", "{{.ID}}")
		list.WaitDelay = 100 * time.Millisecond
		if ids, listErr := list.Output(); listErr == nil && len(strings.Fields(string(ids))) == 0 {
			return false, false, nil
		}
		return false, false, fmt.Errorf("inspect Collector running state: %w", err)
	}
	switch strings.TrimSpace(string(out)) {
	case "true":
		return true, true, nil
	case "false":
		return false, true, nil
	default:
		return false, false, errors.New("Docker returned an invalid Collector running state")
	}
}

// startDocker returns whether this attempt launched a new container.
func startDocker(ctx context.Context, configPath, dataPath string) (bool, error) {
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return false, err
	}
	collectorConfig, err := config.ReadCollector(absConfig)
	if err != nil {
		return false, err
	}
	configBytes, err := os.ReadFile(absConfig)
	if err != nil {
		return false, err
	}
	cfgHash := fmt.Sprintf("%x", sha256.Sum256(configBytes))
	running, exists, err := inspectDockerState(ctx)
	if err != nil {
		return false, err
	}
	if running {
		state, err := readState()
		if err != nil {
			return false, err
		}
		restart, warning := restartDecision(state, defaultImage, absConfig, cfgHash)
		if !restart {
			if warning != "" {
				fmt.Fprintln(os.Stderr, warning)
			}
			fmt.Printf("Collector is already running (container %s).\n", containerName)
			return false, nil
		}
		fmt.Println("Collector configuration or launch options changed; restarting.")
		if err := stopDocker(ctx); err != nil {
			return false, err
		}
		exists = false
	}

	// Clean up stale container if it exists but isn't running.
	if exists {
		// Do not force removal if an external actor has since started it.
		if err := exec.CommandContext(ctx, "docker", "rm", containerName).Run(); err != nil {
			return false, fmt.Errorf("removing stopped Collector container: %w", err)
		}
	}

	// Also clean up stale state file.
	_ = removeState()

	dockerBin, err := findDocker()
	if err != nil {
		return false, err
	}

	// Ensure data directories exist.
	for _, sub := range []string{"traces", "metrics", "logs", "profiles"} {
		if err := os.MkdirAll(filepath.Join(dataPath, sub), 0o755); err != nil {
			return false, fmt.Errorf("creating data directory %s: %w", sub, err)
		}
	}

	absData, err := filepath.Abs(dataPath)
	if err != nil {
		return false, fmt.Errorf("resolving data path: %w", err)
	}

	mountConfig := absConfig
	if !bytes.Equal(configBytes, collectorConfig.YAML) {
		statePath, err := stateFilePath()
		if err != nil {
			return false, err
		}
		privateDir := filepath.Join(filepath.Dir(statePath), "docker-config")
		if err := os.MkdirAll(privateDir, 0o700); err != nil {
			return false, err
		}
		if err := os.Chmod(privateDir, 0o700); err != nil {
			return false, err
		}
		// Docker mounts only this file; the private parent protects it on the host.
		mountConfig = filepath.Join(privateDir, "collector.yaml")
		if err := os.WriteFile(mountConfig, collectorConfig.YAML, 0o644); err != nil {
			return false, err
		}
		if err := os.Chmod(mountConfig, 0o644); err != nil {
			return false, err
		}
	}
	// Run the container.
	cmd := exec.Command(dockerBin, "run", "-d",
		"--name", containerName,
		"-p", "4317:4317",
		"-p", "4318:4318",
		"-p", "13133:13133",
		"-v", absData+":/data",
		"-v", mountConfig+":/etc/otelcol-contrib/config.yaml:ro",
		defaultImage,
		"--config=/etc/otelcol-contrib/config.yaml",
		"--feature-gates=+service.profilesSupport",
	)
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("starting collector container: %w", err)
	}

	containerID := strings.TrimSpace(string(out))

	newState := &State{
		ContainerID:     containerID,
		ContainerName:   containerName,
		Image:           defaultImage,
		StartedAt:       time.Now(),
		ConfigPath:      absConfig,
		ConfigSHA256:    cfgHash,
		DataPath:        absData,
		ProfilesEnabled: true,
	}
	if err := writeState(newState); err != nil {
		// Remove the container if we can't persist state.
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		return false, fmt.Errorf("persisting state: %w", err)
	}

	fmt.Printf("Collector started (container %s).\n", containerName)
	fmt.Printf("Image:  %s\n", defaultImage)
	fmt.Printf("Config: %s\n", absConfig)
	fmt.Printf("Data:   %s\n", absData)
	fmt.Println("Health: http://localhost:13133/")

	return true, nil
}

func stopDocker(ctx context.Context) error {
	running, exists, err := inspectDockerState(ctx)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Println("No collector is running.")
		return nil
	}
	fmt.Printf("Stopping collector (container %s)...\n", containerName)
	if running {
		if err := exec.CommandContext(ctx, "docker", "stop", containerName).Run(); err != nil {
			// An explicit stop still attempts the existing force-removal fallback.
			removeErr := exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()
			return fmt.Errorf("stopping collector container: %w", errors.Join(err, removeErr))
		}
	}
	if err := exec.CommandContext(ctx, "docker", "rm", containerName).Run(); err != nil {
		return fmt.Errorf("removing collector container: %w", err)
	}
	fmt.Println("Collector stopped.")
	return nil
}

// GetStatus returns the current collector status.
func GetStatus(ctx context.Context) (*Status, error) {
	state, err := readState()
	if err != nil {
		return &Status{}, err
	}

	if state != nil && state.ControlSocket != "" {
		return runtimeStatus(ctx, state)
	}
	status := &Status{Backend: "docker"}
	if !isContainerRunning() {
		return status, nil
	}

	status.Running = true
	if state != nil {
		status.ContainerID = state.ContainerID
		status.ContainerName = state.ContainerName
		status.Image = state.Image
		status.ConfigPath = state.ConfigPath
		status.Uptime = time.Since(state.StartedAt).Truncate(time.Second).String()
	} else {
		status.ContainerName = containerName
	}
	status.Healthy = checkHealth()

	return status, nil
}

// checkHealth probes the collector health endpoint.
func checkHealth() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// WaitHealthy polls the health endpoint until healthy or timeout.
func WaitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := readState()
		if err != nil {
			return err
		}
		if state != nil && state.ControlSocket != "" {
			status, err := runtimeStatus(ctx, state)
			if err != nil {
				return err
			}
			if !status.Running {
				return fmt.Errorf("native collector is not running")
			}
			if status.Healthy {
				return nil
			}
		} else if checkHealth() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("collector did not become healthy within %s", timeout)
}

// ContainerID returns the running collector container ID, or empty if not running.
func ContainerID() string {
	state, _ := readState()
	if state != nil && isContainerRunning() {
		return state.ContainerID
	}
	return ""
}

func restartDecision(s *State, image, cfgPath, cfgHash string) (bool, string) {
	if s == nil || s.Image != image {
		return true, ""
	}
	if s.ConfigPath != cfgPath {
		return false, fmt.Sprintf("collector is running with config %s; this directory resolves %s. Run 'lotel stop' first to switch.", s.ConfigPath, cfgPath)
	}
	return s.ConfigSHA256 != cfgHash || !s.ProfilesEnabled, ""
}
