package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDockerMonitorConfirmedStop(t *testing.T) {
	for name, script := range map[string]string{
		"stopped": "#!/bin/sh\nprintf false\n",
		"removed": "#!/bin/sh\ncase \"$1\" in\ninspect) exit 1;;\nps) exit 0;;\n*) exit 1;;\nesac\n",
	} {
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := monitorDocker(t.Context()); err == nil || err.Error() != "Collector container exited" {
				t.Fatalf("confirmed exit: %v", err)
			}
		})
	}
}

func TestDockerMonitorInspectionFailureAndCancellation(t *testing.T) {
	for _, script := range []string{"#!/bin/sh\nexit 125\n", "#!/bin/sh\nexec /bin/sleep 30\n", "#!/bin/sh\nprintf unknown\n"} {
		t.Run(script, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			start := time.Now()
			if err := monitorDocker(ctx); err != nil {
				t.Fatalf("unknown inspection became confirmed exit: %v", err)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("inspection delayed cancellation: %s", elapsed)
			}
		})
	}
}

func TestDockerInspectionTimeout(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if running, err := inspectDockerRunning(t.Context()); err == nil || running {
		t.Fatalf("hung Docker inspection reported known state: %t %v", running, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Docker inspection exceeded its bounded timeout: %s", elapsed)
	}
}
