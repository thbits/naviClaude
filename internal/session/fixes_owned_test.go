package session

import (
	"testing"
	"time"
)

// TestParseResumeFlagInlineForm covers the --resume=<uuid> form added alongside
// the existing space-separated --resume <uuid> form. (The space-separated cases
// are exercised by TestParseResumeFlag in detector_test.go.)
func TestParseResumeFlagInlineForm(t *testing.T) {
	tests := []struct {
		name    string
		cmdLine string
		want    string
	}{
		{
			name:    "inline equals form",
			cmdLine: "claude --resume=abc-123-def",
			want:    "abc-123-def",
		},
		{
			name:    "inline form with other args",
			cmdLine: "/usr/bin/claude --verbose --resume=my-session-id --json",
			want:    "my-session-id",
		},
		{
			name:    "inline form empty value",
			cmdLine: "claude --resume=",
			want:    "",
		},
		{
			name:    "inline form with uuid containing no extra equals",
			cmdLine: "claude --resume=11111111-2222-3333-4444-555555555555",
			want:    "11111111-2222-3333-4444-555555555555",
		},
		{
			name:    "space form still works after adding inline",
			cmdLine: "claude --resume space-id",
			want:    "space-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResumeFlag(tt.cmdLine)
			if got != tt.want {
				t.Errorf("parseResumeFlag(%q) = %q, want %q", tt.cmdLine, got, tt.want)
			}
		})
	}
}

// TestShellSingleQuoteEscaping verifies the cwd quoting helper produces a single
// shell token that suppresses expansion of $, backticks, and backslashes, and
// that embedded single quotes are escaped with the close-escape-reopen sequence.
func TestShellSingleQuoteEscaping(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain path", "/home/user/project", "'/home/user/project'"},
		{"path with space", "/home/my project", "'/home/my project'"},
		{"path with dollar", "/home/$HOME/x", "'/home/$HOME/x'"},
		{"path with backtick", "/home/`whoami`/x", "'/home/`whoami`/x'"},
		{"path with backslash", `/home/a\b`, `'/home/a\b'`},
		{"path with double quote", `/home/a"b`, `'/home/a"b'`},
		{"empty", "", "''"},
		{"single quote in path", "/home/o'brien", `'/home/o'\''brien'`},
		{"only a single quote", "'", `''\'''`},
		{"two single quotes", "''", `''\'''\'''`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellSingleQuote(tt.in)
			if got != tt.want {
				t.Errorf("shellSingleQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestStatusTrackerPruneDropsClosedTargets verifies Prune removes hysteresis
// entries for targets not in the live set while preserving live ones, and that
// status output is unaffected.
func TestStatusTrackerPruneDropsClosedTargets(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	st := NewStatusTracker(10 * time.Second)

	// Two targets become working, so both get hysteresis entries.
	st.Resolve("live", SignalWorking, false, false, t0)
	st.Resolve("closed", SignalWorking, false, false, t0)

	if len(st.lastWorking) != 2 {
		t.Fatalf("setup: lastWorking has %d entries, want 2", len(st.lastWorking))
	}

	// Prune with only "live" in the active set.
	st.Prune(map[string]bool{"live": true})

	if _, ok := st.lastWorking["closed"]; ok {
		t.Error("closed target should have been pruned")
	}
	if _, ok := st.lastWorking["live"]; !ok {
		t.Error("live target should have been kept")
	}

	// Status output is unchanged for the live target: still within grace -> Active.
	if got := st.Resolve("live", SignalNone, false, false, t0.Add(1*time.Second)); got != StatusActive {
		t.Errorf("live after prune: got %v, want StatusActive (hysteresis preserved)", got)
	}
	// The pruned target starts fresh: no working observation -> Idle.
	if got := st.Resolve("closed", SignalNone, false, false, t0.Add(1*time.Second)); got != StatusIdle {
		t.Errorf("pruned target: got %v, want StatusIdle (fresh start)", got)
	}
}

// TestStatusTrackerPruneEmptyActiveClearsAll verifies that pruning with an empty
// active set removes every entry, and that pruning is a no-op when nothing is
// tracked.
func TestStatusTrackerPruneEmptyActiveClearsAll(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	st := NewStatusTracker(10 * time.Second)
	st.Resolve("a", SignalWorking, false, false, t0)
	st.Resolve("b", SignalWorking, false, false, t0)

	st.Prune(map[string]bool{})
	if len(st.lastWorking) != 0 {
		t.Errorf("after prune with empty active: %d entries, want 0", len(st.lastWorking))
	}

	// No-op on empty tracker.
	st.Prune(nil)
	if len(st.lastWorking) != 0 {
		t.Errorf("prune(nil) on empty tracker: %d entries, want 0", len(st.lastWorking))
	}
}

// TestNativeStatusTrustedRegardlessOfTimestampAge is a regression test for a
// false-WAITING bug. Claude writes statusUpdatedAt only when the status CHANGES
// (event-driven), not as a heartbeat, so a session sitting stably idle or busy
// legitimately has an old timestamp -- an actively-busy session was observed
// with a 156s-old timestamp. resolveStatus must therefore honor a present
// native status regardless of its timestamp age; otherwise a stable session is
// demoted to the content-classification fallback, which can false-positive
// WAITING on idle pane content (the input box / leftover scrollback).
func TestNativeStatusTrustedRegardlessOfTimestampAge(t *testing.T) {
	d := NewDetector(nil, nil, 0, 0)
	emptyTree := &ProcessTree{
		children: map[int][]int{},
		names:    map[int]string{},
		ppid:     map[int]int{},
		cpu:      map[int]float64{},
		rss:      map[int]float64{},
	}
	// A timestamp far older than any plausible cutoff, with no tmux target so
	// resolveStatus never captures pane content -- the ONLY thing that can yield
	// a non-idle result is the trusted native status.
	staleMs := time.Now().Add(-30 * time.Minute).UnixMilli()
	cases := []struct {
		native string
		want   SessionStatus
	}{
		{"busy", StatusActive},
		{"idle", StatusIdle},
		{"waiting", StatusWaiting},
	}
	for _, c := range cases {
		s := &Session{nativeStatus: c.native, nativeStatusAt: staleMs}
		if got := d.resolveStatus(s, emptyTree); got != c.want {
			t.Errorf("native %q with a 30m-old timestamp: resolveStatus = %v, want %v "+
				"(native status must be trusted regardless of age)", c.native, got, c.want)
		}
	}
}
