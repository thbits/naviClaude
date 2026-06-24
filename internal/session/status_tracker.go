package session

import "time"

// StatusTracker resolves a final SessionStatus from the per-cycle signals,
// applying priority (Waiting > Working > Idle) and hysteresis so a momentary
// dropout of the working signal between captures does not flicker to Idle.
type StatusTracker struct {
	grace       time.Duration
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

// Resolve returns the status for target given this cycle's signals.
//   - signal is the content classification (Waiting/Working/None).
//   - cpuActive is true when the process subtree CPU is above threshold.
//   - transcriptActive is true when the .jsonl was written within the window.
//
// Waiting always wins and clears immediately when the prompt disappears.
// Working is sticky for grace to absorb capture-to-capture dropouts.
func (st *StatusTracker) Resolve(target string, signal PaneSignal, cpuActive, transcriptActive bool, now time.Time) SessionStatus {
	// Waiting wins and never sticks: as soon as the prompt is gone, it clears.
	if signal == SignalWaiting {
		return StatusWaiting
	}

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
