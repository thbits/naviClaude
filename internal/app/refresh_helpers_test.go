package app

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

// buildActiveIDSet must collect only non-empty IDs into the exclusion set used
// by the closed/history scans.
func TestBuildActiveIDSet(t *testing.T) {
	active := []*session.Session{
		{ID: "a"},
		{ID: ""}, // placeholder with no ID yet -- must be skipped
		{ID: "b"},
	}
	got := buildActiveIDSet(active)
	if len(got) != 2 {
		t.Fatalf("set size = %d, want 2 (%v)", len(got), got)
	}
	if !got["a"] || !got["b"] {
		t.Errorf("set = %v, want {a,b}", got)
	}
	if got[""] {
		t.Errorf("empty ID must not be in the set: %v", got)
	}
}

// mergePendingPlaceholder must carry a not-yet-detected new session forward at
// the FRONT of the active list (matching where creation prepended it) so the
// new session doesn't visibly jump on the next refresh.
func TestMergePendingPlaceholderPrependsCarriedPlaceholder(t *testing.T) {
	placeholder := &session.Session{TmuxTarget: "sess:9.0", ProjectName: "new"}
	m := Model{
		pendingNewTarget: "sess:9.0",
		sessions:         []*session.Session{placeholder},
	}

	// The fresh active scan hasn't picked up the new pane yet.
	active := []*session.Session{
		{TmuxTarget: "sess:1.0"},
		{TmuxTarget: "sess:2.0"},
	}

	got := m.mergePendingPlaceholder(active)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != placeholder {
		t.Errorf("placeholder must be first; got[0].TmuxTarget = %q", got[0].TmuxTarget)
	}
	// pendingNewTarget is still set because the placeholder wasn't detected.
	if m.pendingNewTarget != "sess:9.0" {
		t.Errorf("pendingNewTarget = %q, want sess:9.0 (still pending)", m.pendingNewTarget)
	}
}

// Once the new session appears in the active scan, the placeholder is dropped
// and pendingNewTarget cleared.
func TestMergePendingPlaceholderClearsOnceDetected(t *testing.T) {
	m := Model{
		pendingNewTarget: "sess:9.0",
		sessions:         []*session.Session{{TmuxTarget: "sess:9.0"}},
	}
	active := []*session.Session{
		{TmuxTarget: "sess:9.0", ID: "real"},
	}
	got := m.mergePendingPlaceholder(active)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (no duplicate placeholder)", len(got))
	}
	if m.pendingNewTarget != "" {
		t.Errorf("pendingNewTarget = %q, want cleared", m.pendingNewTarget)
	}
}
