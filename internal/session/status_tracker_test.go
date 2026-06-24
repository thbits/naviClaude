package session

import (
	"testing"
	"time"
)

func TestStatusTrackerResolve(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)

	t.Run("waiting signal wins over everything", func(t *testing.T) {
		st := NewStatusTracker(10 * time.Second)
		got := st.Resolve("a", SignalWaiting, true, true, t0)
		if got != StatusWaiting {
			t.Errorf("got %v, want StatusWaiting", got)
		}
	})

	t.Run("working signal yields active", func(t *testing.T) {
		st := NewStatusTracker(10 * time.Second)
		got := st.Resolve("a", SignalWorking, false, false, t0)
		if got != StatusActive {
			t.Errorf("got %v, want StatusActive", got)
		}
	})

	t.Run("cpu active yields active without content signal", func(t *testing.T) {
		st := NewStatusTracker(10 * time.Second)
		got := st.Resolve("a", SignalNone, true, false, t0)
		if got != StatusActive {
			t.Errorf("got %v, want StatusActive", got)
		}
	})

	t.Run("transcript active yields active", func(t *testing.T) {
		st := NewStatusTracker(10 * time.Second)
		got := st.Resolve("a", SignalNone, false, true, t0)
		if got != StatusActive {
			t.Errorf("got %v, want StatusActive", got)
		}
	})

	t.Run("nothing active yields idle", func(t *testing.T) {
		st := NewStatusTracker(10 * time.Second)
		got := st.Resolve("a", SignalNone, false, false, t0)
		if got != StatusIdle {
			t.Errorf("got %v, want StatusIdle", got)
		}
	})
}

func TestStatusTrackerHysteresis(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	st := NewStatusTracker(10 * time.Second)

	// Working observed at t0.
	if got := st.Resolve("a", SignalWorking, false, false, t0); got != StatusActive {
		t.Fatalf("t0: got %v, want StatusActive", got)
	}

	// 9s later with no signal: still within grace -> stays Active.
	if got := st.Resolve("a", SignalNone, false, false, t0.Add(9*time.Second)); got != StatusActive {
		t.Errorf("t0+9s: got %v, want StatusActive (hysteresis)", got)
	}

	// 11s after the LAST working observation: grace expired -> Idle.
	// (last working was t0 because t0+9s carried no working signal.)
	if got := st.Resolve("a", SignalNone, false, false, t0.Add(11*time.Second)); got != StatusIdle {
		t.Errorf("t0+11s: got %v, want StatusIdle (grace expired)", got)
	}
}

func TestStatusTrackerWaitingClearsImmediately(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	st := NewStatusTracker(10 * time.Second)

	if got := st.Resolve("a", SignalWaiting, false, false, t0); got != StatusWaiting {
		t.Fatalf("t0: got %v, want StatusWaiting", got)
	}
	// Prompt gone, nothing else active: immediately Idle (no waiting stickiness).
	if got := st.Resolve("a", SignalNone, false, false, t0.Add(1*time.Second)); got != StatusIdle {
		t.Errorf("t0+1s: got %v, want StatusIdle", got)
	}
}

func TestStatusTrackerPerTargetIsolation(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	st := NewStatusTracker(10 * time.Second)

	st.Resolve("a", SignalWorking, false, false, t0)
	// Target b has never been working -> Idle even though a is active.
	if got := st.Resolve("b", SignalNone, false, false, t0); got != StatusIdle {
		t.Errorf("target b: got %v, want StatusIdle", got)
	}
}
