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
	"strconv"
	"strings"
	"syscall"
	"time"
)

// State represents the persisted state of a running collector process.
type State struct {
	PID       int       `json:"pid"`
	Binary    string    `json:"binary"`
	StartedAt time.Time `json:"started_at"`
	ConfigPath string   `json:"config_path"`
	DataPath   string   `json:"data_path"`
}

// Status represents the current status of the collector.
type Status struct {
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	Healthy   bool   `json:"healthy"`
	Uptime    string `json:"uptime,omitempty"`
	Binary    string `json:"binary,omitempty"`
}

const (
	stateDir  = ".lotel"
	stateFile = "collector.state"
	healthURL = "http://localhost:13133/"
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

// isProcessAlive checks if a process with the given PID is alive
// and is actually an otelcol process.
func isProcessAlive(s *State) bool {
	if s == nil || s.PID == 0 {
		return false
	}
	proc, err := os.FindProcess(s.PID)
	if err != nil {
		return false
	}
	// Signal 0 checks process existence without sending a signal.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	// Verify it's actually our collector process by checking /proc/{pid}/cmdline.
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", s.PID))
	if err != nil {
		// On non-Linux or if /proc unavailable, fall back to trusting PID.
		return true
	}
	return strings.Contains(string(cmdline), "otelcol")
}

// findBinary locates the otelcol-contrib or otelcol binary.
func findBinary() (string, error) {
	for _, name := range []string{"otelcol-contrib", "otelcol"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("otelcol-contrib not found in PATH; install from https://github.com/open-telemetry/opentelemetry-collector-releases")
}

// Start launches the collector as a background subprocess.
func Start(ctx context.Context, configPath, dataPath string) error {
	// Check if already running.
	state, err := readState()
	if err != nil {
		return err
	}
	if isProcessAlive(state) {
		fmt.Printf("Collector is already running (PID %d).\n", state.PID)
		return nil
	}
	// Clean up stale state if process is dead.
	if state != nil {
		_ = removeState()
	}

	binary, err := findBinary()
	if err != nil {
		return err
	}

	// Ensure data directories exist.
	for _, sub := range []string{"traces", "metrics", "logs"} {
		if err := os.MkdirAll(filepath.Join(dataPath, sub), 0o755); err != nil {
			return fmt.Errorf("creating data directory %s: %w", sub, err)
		}
	}

	// Build the collector config with resolved data paths.
	resolvedConfig, err := resolveConfig(configPath, dataPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	cmd := exec.Command(binary, "--config", resolvedConfig)
	cmd.Stdout = nil // Collector logs to stderr by default.
	cmd.Stderr = nil

	// Detach from parent process group so the collector survives CLI exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting collector: %w", err)
	}

	newState := &State{
		PID:        cmd.Process.Pid,
		Binary:     binary,
		StartedAt:  time.Now(),
		ConfigPath: resolvedConfig,
		DataPath:   dataPath,
	}
	if err := writeState(newState); err != nil {
		// Kill the process if we can't persist state.
		_ = cmd.Process.Kill()
		return fmt.Errorf("persisting state: %w", err)
	}

	// Release the process so it's not tied to this CLI invocation.
	_ = cmd.Process.Release()

	fmt.Printf("Collector started (PID %d).\n", newState.PID)
	fmt.Printf("Binary: %s\n", binary)
	fmt.Printf("Config: %s\n", resolvedConfig)
	fmt.Printf("Data:   %s\n", dataPath)
	fmt.Println("Health: http://localhost:13133/")

	return nil
}

// Stop terminates the running collector.
func Stop(ctx context.Context) error {
	state, err := readState()
	if err != nil {
		return err
	}
	if state == nil || !isProcessAlive(state) {
		_ = removeState()
		fmt.Println("No collector is running.")
		return nil
	}

	proc, err := os.FindProcess(state.PID)
	if err != nil {
		_ = removeState()
		return fmt.Errorf("finding process %d: %w", state.PID, err)
	}

	// Send SIGTERM for graceful shutdown.
	fmt.Printf("Stopping collector (PID %d)...\n", state.PID)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		_ = removeState()
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	// Wait up to 10 seconds for graceful shutdown.
	done := make(chan error, 1)
	go func() {
		// Poll for process exit.
		for i := 0; i < 100; i++ {
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				done <- nil
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		done <- fmt.Errorf("process did not exit within 10s")
	}()

	if err := <-done; err != nil {
		// Force kill.
		fmt.Println("Graceful shutdown timed out, sending SIGKILL...")
		_ = proc.Signal(syscall.SIGKILL)
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
	if state == nil || !isProcessAlive(state) {
		_ = removeState()
		return status, nil
	}

	status.Running = true
	status.PID = state.PID
	status.Binary = state.Binary
	status.Uptime = time.Since(state.StartedAt).Truncate(time.Second).String()
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

// resolveConfig creates a runtime config file with resolved data paths.
// It reads the source config and writes a copy with ${DATA_DIR} replaced.
func resolveConfig(configPath, dataPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("reading config %s: %w", configPath, err)
	}

	content := string(data)
	// Replace /data/ prefix in paths with the actual data directory.
	content = strings.ReplaceAll(content, "/data/", dataPath+"/")

	// Write resolved config to state directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	resolvedPath := filepath.Join(home, stateDir, "collector-config.yaml")
	if err := os.WriteFile(resolvedPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing resolved config: %w", err)
	}
	return resolvedPath, nil
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

// Pid returns the running collector PID as a string, or empty if not running.
func Pid() string {
	state, _ := readState()
	if state != nil && isProcessAlive(state) {
		return strconv.Itoa(state.PID)
	}
	return ""
}
