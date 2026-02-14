package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// State represents the persisted state of a running collector container.
type State struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Image         string    `json:"image"`
	StartedAt     time.Time `json:"started_at"`
	ConfigPath    string    `json:"config_path"`
	DataPath      string    `json:"data_path"`
}

// Status represents the current status of the collector.
type Status struct {
	Running       bool   `json:"running"`
	ContainerID   string `json:"container_id,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	Healthy       bool   `json:"healthy"`
	Uptime        string `json:"uptime,omitempty"`
	Image         string `json:"image,omitempty"`
}

const (
	stateDir  = ".lotel"
	stateFile = "collector.state"
	healthURL = "http://localhost:13133/"

	containerName = "lotel-collector"
	defaultImage  = "otel/opentelemetry-collector-contrib:latest"
)

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
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
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
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// containerExists checks if the lotel-collector container exists (any state).
func containerExists() bool {
	err := exec.Command("docker", "inspect", containerName).Run()
	return err == nil
}

// Start launches the collector as a Docker container.
func Start(ctx context.Context, configPath, dataPath string) error {
	// Check if already running.
	if isContainerRunning() {
		fmt.Printf("Collector is already running (container %s).\n", containerName)
		return nil
	}

	// Clean up stale container if it exists but isn't running.
	if containerExists() {
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
	}

	// Also clean up stale state file.
	_ = removeState()

	dockerBin, err := findDocker()
	if err != nil {
		return err
	}

	// Ensure data directories exist.
	for _, sub := range []string{"traces", "metrics", "logs"} {
		if err := os.MkdirAll(filepath.Join(dataPath, sub), 0o755); err != nil {
			return fmt.Errorf("creating data directory %s: %w", sub, err)
		}
	}

	// Resolve to absolute paths for Docker bind mounts.
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}
	absData, err := filepath.Abs(dataPath)
	if err != nil {
		return fmt.Errorf("resolving data path: %w", err)
	}

	// Run the container.
	cmd := exec.Command(dockerBin, "run", "-d",
		"--name", containerName,
		"-p", "4317:4317",
		"-p", "4318:4318",
		"-p", "13133:13133",
		"-v", absData+":/data",
		"-v", absConfig+":/etc/otelcol-contrib/config.yaml:ro",
		defaultImage,
	)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("starting collector container: %w", err)
	}

	containerID := strings.TrimSpace(string(out))

	newState := &State{
		ContainerID:   containerID,
		ContainerName: containerName,
		Image:         defaultImage,
		StartedAt:     time.Now(),
		ConfigPath:    absConfig,
		DataPath:      absData,
	}
	if err := writeState(newState); err != nil {
		// Remove the container if we can't persist state.
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		return fmt.Errorf("persisting state: %w", err)
	}

	fmt.Printf("Collector started (container %s).\n", containerName)
	fmt.Printf("Image:  %s\n", defaultImage)
	fmt.Printf("Config: %s\n", absConfig)
	fmt.Printf("Data:   %s\n", absData)
	fmt.Println("Health: http://localhost:13133/")

	return nil
}

// Stop terminates the running collector container.
func Stop(ctx context.Context) error {
	if !isContainerRunning() && !containerExists() {
		_ = removeState()
		fmt.Println("No collector is running.")
		return nil
	}

	fmt.Printf("Stopping collector (container %s)...\n", containerName)

	if err := exec.Command("docker", "stop", containerName).Run(); err != nil {
		// If stop fails, try force remove.
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		_ = removeState()
		return fmt.Errorf("stopping collector container: %w", err)
	}

	if err := exec.Command("docker", "rm", containerName).Run(); err != nil {
		_ = removeState()
		return fmt.Errorf("removing collector container: %w", err)
	}

	_ = removeState()
	fmt.Println("Collector stopped.")
	return nil
}

// GetStatus returns the current collector status.
func GetStatus(ctx context.Context) (*Status, error) {
	state, err := readState()
	if err != nil {
		return &Status{}, err
	}

	status := &Status{}
	if !isContainerRunning() {
		_ = removeState()
		return status, nil
	}

	status.Running = true
	if state != nil {
		status.ContainerID = state.ContainerID
		status.ContainerName = state.ContainerName
		status.Image = state.Image
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
		if checkHealth() {
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
