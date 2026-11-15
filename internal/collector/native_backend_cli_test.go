package collector_test

import (
	"encoding/json"
	"testing"

	"github.com/mattsp1290/lotel/internal/collector"
)

func TestNativeBackendSwitchRequiresStop(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	if out, err := f.run("start", "--backend", "native", "--wait"); err != nil {
		t.Fatalf("native start: %v\n%s", err, out)
	}
	before, err := f.run("status")
	if err != nil {
		t.Fatalf("native status: %v\n%s", err, before)
	}
	var previous collector.Status
	if err := json.Unmarshal(before, &previous); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start", "--backend", "docker"); err == nil {
		t.Fatalf("switched away from a running native collector: %s", out)
	}
	after, err := f.run("status")
	if err != nil {
		t.Fatalf("native status after rejected switch: %v\n%s", err, after)
	}
	var current collector.Status
	if err := json.Unmarshal(after, &current); err != nil {
		t.Fatal(err)
	}
	if !current.Running || !current.Healthy || current.PID != previous.PID {
		t.Fatalf("rejected backend switch changed the native instance: %+v -> %+v", previous, current)
	}
}
