package collector_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type nativeCLIFixture struct {
	bin      string
	home     string
	dir      string
	endpoint string
	env      []string
}

func newNativeCLIFixture(t *testing.T, processors string) nativeCLIFixture {
	t.Helper()
	bin := os.Getenv("LOTEL_NATIVE_TEST_BIN")
	if bin == "" {
		t.Skip("set LOTEL_NATIVE_TEST_BIN to a built lotel executable")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	f := nativeCLIFixture{bin: bin, home: t.TempDir(), dir: t.TempDir()}
	ports := make([]string, 3)
	for i := range ports {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports[i] = listener.Addr().String()
		listener.Close()
	}
	f.endpoint = "http://" + ports[1]
	config := fmt.Sprintf(`receivers:
  otlp:
    protocols:
      grpc:
        endpoint: %s
      http:
        endpoint: %s
processors:
  batch:
    timeout: 5s
%s
exporters:
  file/logs:
    path: /data/logs/logs.jsonl
    append: true
extensions:
  health_check:
    endpoint: %s
service:
  extensions: [health_check]
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/logs]
  telemetry:
    metrics:
      level: none
ingestion:
  enabled: true
  interval: 1h
`, ports[0], ports[1], processors, ports[2])
	if processors != "" {
		config = strings.Replace(config, "processors: [batch]", "processors: [transform, batch]", 1)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "lotel-collector.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	f.env = append(os.Environ(), "HOME="+f.home, "PATH="+filepath.Join(f.home, "no-executables"))
	t.Cleanup(func() {
		if out, err := f.run("stop"); err != nil {
			t.Errorf("cleanup stop: %v\n%s", err, out)
		}
	})
	return f
}

func (f nativeCLIFixture) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, f.bin, args...)
	cmd.Dir, cmd.Env = f.dir, f.env
	return cmd
}

func (f nativeCLIFixture) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return f.command(ctx, args...).CombinedOutput()
}
