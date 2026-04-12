package version

import "testing"

func TestVersionDefaults(t *testing.T) {
	if Version != "dev" {
		t.Errorf("Version = %q, want %q", Version, "dev")
	}
	if GitCommit != "unknown" {
		t.Errorf("GitCommit = %q, want %q", GitCommit, "unknown")
	}
	if BuildTime != "unknown" {
		t.Errorf("BuildTime = %q, want %q", BuildTime, "unknown")
	}
}
