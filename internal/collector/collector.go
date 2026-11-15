package collector

import (
	"context"
	"crypto/sha256"
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
	ContainerID     string    `json:"container_id"`
	ContainerName   string    `json:"container_name"`
	Image           string    `json:"image"`
	StartedAt       time.Time `json:"started_at"`
	ConfigSHA256    string    `json:"config_sha256"`
	ConfigPath      string    `json:"config_path"`
	DataPath        string    `json:"data_path"`
	ProfilesEnabled bool      `json:"profiles_enabled"`
}

// Status represents the current status of the collector.
type Status struct {
	ConfigPath    string `json:"config_path,omitempty"`
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

func InspectForUpgrade(ctx context.Context) (*Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	docker, err := findDocker()
	if err != nil {
		return nil, err
	}
	list := exec.CommandContext(ctx, docker, "ps", "--all", "--filter", "name=^/"+containerName+"$", "--format", "{{.ID}}")
	list.WaitDelay = time.Second
	out, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Collector container list: %w", err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return &Status{}, nil
	}
	if len(ids) != 1 || len(ids[0]) < 12 || len(ids[0]) > 64 || strings.Trim(ids[0], "0123456789abcdef") != "" {
		return nil, fmt.Errorf("Docker returned an invalid Collector container ID")
	}
	inspect := exec.CommandContext(ctx, docker, "inspect", "--format", "{{.State.Running}}", ids[0])
	inspect.WaitDelay = time.Second
	out, err = inspect.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Collector running state: %w", err)
	}
	switch strings.TrimSpace(string(out)) {
	case "false":
		return &Status{}, nil
	case "true":
	default:
		return nil, fmt.Errorf("Docker returned an invalid Collector running state")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	state, err := readStateFile(filepath.Join(home, stateDir, stateFile))
	if err != nil {
		return nil, err
	}
	if state == nil || state.ConfigPath == "" {
		return nil, fmt.Errorf("running Collector has no recorded config path; restart it with lotel before upgrading")
	}
	return &Status{Running: true, ConfigPath: state.ConfigPath}, nil
}

// Start launches the collector as a Docker container.
func Start(ctx context.Context, configPath, dataPath string) error {
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	configBytes, err := os.ReadFile(absConfig)
	if err != nil {
		return err
	}
	cfgHash := fmt.Sprintf("%x", sha256.Sum256(configBytes))
	if isContainerRunning() {
		state, err := readState()
		if err != nil {
			return err
		}
		restart, warning := restartDecision(state, defaultImage, absConfig, cfgHash)
		if !restart {
			if warning != "" {
				fmt.Fprintln(os.Stderr, warning)
			}
			fmt.Printf("Collector is already running (container %s).\n", containerName)
			return nil
		}
		fmt.Println("Collector configuration or launch options changed; restarting.")
		if err := Stop(ctx); err != nil {
			return err
		}
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
	for _, sub := range []string{"traces", "metrics", "logs", "profiles"} {
		if err := os.MkdirAll(filepath.Join(dataPath, sub), 0o755); err != nil {
			return fmt.Errorf("creating data directory %s: %w", sub, err)
		}
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
		"--config=/etc/otelcol-contrib/config.yaml",
		"--feature-gates=+service.profilesSupport",
	)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("starting collector container: %w", err)
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

func restartDecision(s *State, image, cfgPath, cfgHash string) (bool, string) {
	if s == nil || s.Image != image {
		return true, ""
	}
	if s.ConfigPath != cfgPath {
		return false, fmt.Sprintf("collector is running with config %s; this directory resolves %s. Run 'lotel stop' first to switch.", s.ConfigPath, cfgPath)
	}
	return s.ConfigSHA256 != cfgHash || !s.ProfilesEnabled, ""
}
