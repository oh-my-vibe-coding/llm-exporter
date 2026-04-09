package status

import (
	"sync"
	"time"
)

// TargetStatus represents the current state of a probe target.
type TargetStatus struct {
	Name           string  `json:"name"`
	Model          string  `json:"model"`
	Endpoint       string  `json:"endpoint"`
	APIFormat      string  `json:"api_format"`
	LastProbeTime  string  `json:"last_probe_time"`
	Success        bool    `json:"last_success"`
	Error          string  `json:"last_error,omitempty"`
	Duration       float64 `json:"last_duration_seconds"`
	TTFT           float64 `json:"last_ttft_seconds"`
	ConsecFailures int     `json:"consecutive_failures"`
	TotalProbes    int64   `json:"total_probes"`
	TotalSuccesses int64   `json:"total_successes"`
}

// ProbeOutcome carries the minimal probe result needed to update status.
type ProbeOutcome struct {
	Success  bool
	Duration time.Duration
	TTFT     time.Duration
	Error    error
}

// Tracker maintains per-target status with thread-safe access.
type Tracker struct {
	mu       sync.RWMutex
	statuses map[string]*TargetStatus
}

func NewTracker() *Tracker {
	return &Tracker{statuses: make(map[string]*TargetStatus)}
}

// Update records a probe outcome for the named target.
func (tr *Tracker) Update(name, model, endpoint, apiFormat string, outcome ProbeOutcome) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	st, ok := tr.statuses[name]
	if !ok {
		st = &TargetStatus{
			Name:      name,
			Model:     model,
			Endpoint:  endpoint,
			APIFormat: apiFormat,
		}
		tr.statuses[name] = st
	}

	st.LastProbeTime = time.Now().Format(time.RFC3339)
	st.Success = outcome.Success
	st.Duration = outcome.Duration.Seconds()
	st.TTFT = outcome.TTFT.Seconds()
	st.TotalProbes++

	if outcome.Success {
		st.ConsecFailures = 0
		st.TotalSuccesses++
		st.Error = ""
	} else {
		st.ConsecFailures++
		if outcome.Error != nil {
			st.Error = outcome.Error.Error()
		}
	}
}

// GetAll returns a snapshot of all target statuses.
func (tr *Tracker) GetAll() []TargetStatus {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	result := make([]TargetStatus, 0, len(tr.statuses))
	for _, st := range tr.statuses {
		result = append(result, *st)
	}
	return result
}

// ConsecFailures returns the consecutive failure count for a target.
func (tr *Tracker) ConsecFailures(name string) (int, *TargetStatus) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	st, ok := tr.statuses[name]
	if !ok {
		return 0, nil
	}
	cp := *st
	return st.ConsecFailures, &cp
}

// Reset clears all statuses.
func (tr *Tracker) Reset() {
	tr.mu.Lock()
	tr.statuses = make(map[string]*TargetStatus)
	tr.mu.Unlock()
}
