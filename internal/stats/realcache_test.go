package stats

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests decode raw JSON matching the *real* shape of
// ~/.claude/stats-cache.json (captured from a live file) rather than
// marshaling the internal struct. Marshaling the struct would round-trip
// whatever tags the struct declares and could never catch a tag mismatch or a
// date-format assumption -- exactly the two bugs these guard against.

// TestComputeAvgPerDayFromRFC3339Date pins the avg/day fix: the real cache
// stores firstSessionDate as a full RFC3339 timestamp, not a "2006-01-02"
// date, so a date-only parse fails and leaves AvgSessionsPerDay at 0.
func TestComputeAvgPerDayFromRFC3339Date(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

	// firstSessionDate as it actually appears in the file (RFC3339 with ms + Z);
	// lastComputedDate as it actually appears (date-only).
	raw := `{
	  "totalSessions": 80,
	  "totalMessages": 16476,
	  "firstSessionDate": "2026-01-29T11:07:33.236Z",
	  "lastComputedDate": "2026-01-30"
	}`
	if err := os.WriteFile(filepath.Join(dir, "stats-cache.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Compute(dir, 0, "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.AvgSessionsPerDay <= 0 {
		t.Errorf("AvgSessionsPerDay = %v, want > 0; RFC3339 firstSessionDate must parse", st.AvgSessionsPerDay)
	}
}

// TestModelUsageIncludesCacheTokens pins the model-usage fix: the real cache
// keys are cacheReadInputTokens / cacheCreationInputTokens, and usage must
// count them alongside input+output (cache is ~99.9% of Claude Code tokens).
func TestModelUsageIncludesCacheTokens(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

	raw := `{
	  "totalSessions": 1,
	  "firstSessionDate": "2026-01-29T11:07:33.236Z",
	  "modelUsage": {
	    "claude-opus-4-6": {
	      "inputTokens": 100,
	      "outputTokens": 200,
	      "cacheReadInputTokens": 5000,
	      "cacheCreationInputTokens": 700
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "stats-cache.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Compute(dir, 0, "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(st.ModelUsage) != 1 {
		t.Fatalf("ModelUsage entries = %d, want 1: %+v", len(st.ModelUsage), st.ModelUsage)
	}
	want := 100 + 200 + 5000 + 700
	if st.ModelUsage[0].Count != want {
		t.Errorf("opus Count = %d, want %d (must include cacheRead + cacheCreation)", st.ModelUsage[0].Count, want)
	}
}
