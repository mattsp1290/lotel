package collector

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsLifecycleBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles Windows lifecycle code")
	}
	dir := t.TempDir()
	for _, name := range []string{"lifecycle.go", "runtime_process.go", "runtime_control.go", "platform_windows.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stubs := `package collector
import("context";"time")
type State struct {ContainerID,ContainerName,Image string;ProfilesEnabled bool;Backend string;PID int;ControlSocket,ControlToken string;StartedAt time.Time;ConfigPath,ConfigSHA256,DataPath string}
type Status struct {ContainerID,ContainerName,Image,ConfigPath string;Backend string;Running,Healthy bool;Uptime string;Ingestion struct{Enabled bool;LastError string}}
type runtimeShutdownResult struct {Error string}
func stateFilePath()(string,error){return "",nil}
func readState()(*State,error){return nil,nil}
func writeState(*State)error{return nil}
func removeState()error{return nil}
func startDocker(context.Context,string,string)(bool,error){return false,nil}
func stopDocker(context.Context)error{return nil}
func isContainerRunning()bool{return false}
func inspectDockerRunning(context.Context)(bool,error){return false,nil}
func finishUnmanagedDocker(context.Context,*State)error{return nil}
`
	if err := os.WriteFile(filepath.Join(dir, "docker_stubs.go"), []byte(stubs), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "windows.a"), filepath.Join(dir, "lifecycle.go"), filepath.Join(dir, "runtime_process.go"), filepath.Join(dir, "runtime_control.go"), filepath.Join(dir, "platform_windows.go"), filepath.Join(dir, "docker_stubs.go"))
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Windows lifecycle compilation: %v\n%s", err, out)
	}
}
