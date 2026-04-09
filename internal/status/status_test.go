package status

import (
	"fmt"
	"testing"
	"time"
)

func TestTracker_Update_NewTarget(t *testing.T) {
	tr := NewTracker()
	tr.Update("test", "gpt-4", "https://api.example.com", "openai", ProbeOutcome{
		Success:  true,
		Duration: 500 * time.Millisecond,
		TTFT:     100 * time.Millisecond,
	})

	all := tr.GetAll()
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	st := all[0]
	if st.Name != "test" {
		t.Errorf("Name = %q, want %q", st.Name, "test")
	}
	if !st.Success {
		t.Error("Success = false, want true")
	}
	if st.TotalProbes != 1 {
		t.Errorf("TotalProbes = %d, want 1", st.TotalProbes)
	}
	if st.TotalSuccesses != 1 {
		t.Errorf("TotalSuccesses = %d, want 1", st.TotalSuccesses)
	}
}

func TestTracker_ConsecutiveFailures(t *testing.T) {
	tr := NewTracker()

	for i := 0; i < 3; i++ {
		tr.Update("t", "m", "e", "openai", ProbeOutcome{
			Error: fmt.Errorf("fail %d", i),
		})
	}

	count, st := tr.ConsecFailures("t")
	if count != 3 {
		t.Errorf("ConsecFailures = %d, want 3", count)
	}
	if st.TotalSuccesses != 0 {
		t.Errorf("TotalSuccesses = %d, want 0", st.TotalSuccesses)
	}

	// Success resets consecutive failures.
	tr.Update("t", "m", "e", "openai", ProbeOutcome{Success: true, Duration: time.Second})
	count, _ = tr.ConsecFailures("t")
	if count != 0 {
		t.Errorf("ConsecFailures after success = %d, want 0", count)
	}
}

func TestTracker_GetAll_Snapshot(t *testing.T) {
	tr := NewTracker()
	tr.Update("a", "m", "e", "openai", ProbeOutcome{Success: true, Duration: time.Second})
	tr.Update("b", "m", "e", "azure", ProbeOutcome{Success: true, Duration: time.Second})

	all := tr.GetAll()
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}

	// Mutating the returned slice should not affect the tracker.
	all[0].Name = "mutated"
	fresh := tr.GetAll()
	for _, st := range fresh {
		if st.Name == "mutated" {
			t.Error("GetAll returned a reference, not a snapshot")
		}
	}
}

func TestTracker_Reset(t *testing.T) {
	tr := NewTracker()
	tr.Update("t", "m", "e", "openai", ProbeOutcome{Success: true, Duration: time.Second})
	tr.Reset()

	all := tr.GetAll()
	if len(all) != 0 {
		t.Errorf("len after Reset = %d, want 0", len(all))
	}
}

func TestTracker_ConsecFailures_Unknown(t *testing.T) {
	tr := NewTracker()
	count, st := tr.ConsecFailures("nonexistent")
	if count != 0 {
		t.Errorf("ConsecFailures = %d, want 0", count)
	}
	if st != nil {
		t.Error("status should be nil for unknown target")
	}
}
