package collector

import "testing"

func TestRestartDecision(t *testing.T) {
	for _, tc := range []struct {
		name             string
		state            *State
		restart, warning bool
	}{
		{"missing", nil, true, false},
		{"image", &State{Image: "old"}, true, false},
		{"path", &State{Image: "image", ConfigPath: "other"}, false, true},
		{"hash", &State{Image: "image", ConfigPath: "config", ConfigSHA256: "old"}, true, false},
		{"current", &State{Image: "image", ConfigPath: "config", ConfigSHA256: "hash"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restart, warning := restartDecision(tc.state, "image", "config", "hash")
			if restart != tc.restart || (warning != "") != tc.warning {
				t.Fatalf("got %v %q", restart, warning)
			}
		})
	}
}
