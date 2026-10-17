package session

import (
	"sync"
	"time"
)

// StatusTracker resolves status by priority (Waiting > Working > Idle), with
// hysteresis so a momentary working dropout doesn't flicker to Idle.
type StatusTracker struct {
	grace time.Duration
	// mu guards lastWorking. Detect runs in overlapping Bubble Tea tea.Cmd
	// goroutines, so Resolve (and Prune) can be called concurrently.
	mu          sync.Mutex
	lastWorking map[string]time.Time
}

// NewStatusTracker creates a tracker that keeps a session marked Active for up
// to grace after the last time a working signal was observed.
func NewStatusTracker(grace time.Duration) *StatusTracker {
	if grace <= 0 {
		grace = 10 * time.Second
	}
	return &StatusTracker{
		grace:       grace,
		lastWorking: make(map[string]time.Time),
	}
}

// Resolve returns target's status from this cycle's pane, CPU and transcript
// signals; Waiting clears at once, while Working sticks for grace.
func (st *StatusTracker) Resolve(target string, signal PaneSignal, cpuActive, transcriptActive bool, now time.Time) SessionStatus {
	// Waiting never sticks, so it needs no map access or lock.
	if signal == SignalWaiting {
		return StatusWaiting
	}

	// Lock narrowly around the only shared-state access: the lastWorking map.
	st.mu.Lock()
	defer st.mu.Unlock()

	workingNow := signal == SignalWorking || cpuActive || transcriptActive
	if workingNow {
		st.lastWorking[target] = now
		return StatusActive
	}

	// Hysteresis: stay Active for grace after the last working observation so a
	// single-frame dropout (footer gone for one capture, CPU dip) doesn't flap.
	if last, ok := st.lastWorking[target]; ok && now.Sub(last) <= st.grace {
		return StatusActive
	}

	return StatusIdle
}

// Prune drops entries for targets not in active so the map doesn't grow as panes
// close; a target that reappears starts fresh.
func (st *StatusTracker) Prune(active map[string]bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for target := range st.lastWorking {
		if !active[target] {
			delete(st.lastWorking, target)
		}
	}
}
