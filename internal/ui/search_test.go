package ui

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

// resultIDs extracts the session IDs from the search results in order.
func resultIDs(results []*session.Session) []string {
	out := make([]string, len(results))
	for i, s := range results {
		out[i] = s.ID
	}
	return out
}

// TestRunSearchStableTieOrder asserts that results with equal scores come back
// in a deterministic order (by ID, then DisplayName) on every run, despite the
// underlying scores being collected from a Go map with random iteration order.
func TestRunSearchStableTieOrder(t *testing.T) {
	// All three sessions share the same project name, so a fuzzy match on the
	// name produces identical scores -- a pure tie broken only by the tiebreaker.
	sessions := []*session.Session{
		{ID: "ccc", ProjectName: "alpha"},
		{ID: "aaa", ProjectName: "alpha"},
		{ID: "bbb", ProjectName: "alpha"},
	}

	m := NewSearch()
	m.SetSessions(sessions)
	m.Activate()
	m.input.SetValue("alpha")

	var want []string
	for run := 0; run < 50; run++ {
		m.runSearch()
		got := resultIDs(m.Results())
		if len(got) != len(sessions) {
			t.Fatalf("run %d: got %d results, want %d (%v)", run, len(got), len(sessions), got)
		}
		if run == 0 {
			want = got
			// The tiebreaker is ID ascending, so the very first run must already
			// be sorted by ID.
			expect := []string{"aaa", "bbb", "ccc"}
			if !equalStrings(want, expect) {
				t.Fatalf("tie order = %v, want %v", want, expect)
			}
			continue
		}
		if !equalStrings(got, want) {
			t.Fatalf("run %d: order = %v, want stable %v", run, got, want)
		}
	}
}

// TestRunSearchScorePriorityWithTie verifies that the stable sort still honors
// score (higher first) and only falls back to the ID tiebreaker within a score
// band.
func TestRunSearchScorePriorityWithTie(t *testing.T) {
	// "zzz" matches on summary (score 2000); the two "alpha" sessions match on
	// project name (score 3000+). The name matches must rank above the summary
	// match, and the two tied name matches must be ID-ordered.
	sessions := []*session.Session{
		{ID: "sum", ProjectName: "other", Summary: "alpha notes"},
		{ID: "n2", ProjectName: "alpha"},
		{ID: "n1", ProjectName: "alpha"},
	}

	m := NewSearch()
	m.SetSessions(sessions)
	m.Activate()
	m.input.SetValue("alpha")
	m.runSearch()

	got := resultIDs(m.Results())
	want := []string{"n1", "n2", "sum"}
	if !equalStrings(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestResultsEmptyQueryIsCopy verifies the empty-query path hands out a copy of
// the session list, so a caller mutating Results() cannot corrupt m.sessions.
func TestResultsEmptyQueryIsCopy(t *testing.T) {
	sessions := []*session.Session{
		{ID: "a", ProjectName: "alpha"},
		{ID: "b", ProjectName: "beta"},
	}

	m := NewSearch()
	m.SetSessions(sessions)
	m.Activate() // empty query -> results should be all sessions, as a copy

	results := m.Results()
	if len(results) != len(sessions) {
		t.Fatalf("got %d results, want %d", len(results), len(sessions))
	}

	// Mutate the returned slice in place (the documented corruption scenario).
	results[0], results[1] = results[1], results[0]

	// The source slice must be untouched.
	if sessions[0].ID != "a" || sessions[1].ID != "b" {
		t.Errorf("m.sessions corrupted by reslicing Results(): order = %s,%s", sessions[0].ID, sessions[1].ID)
	}
}
