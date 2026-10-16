package claude

import (
	"github.com/thbits/naviClaude/internal/stats"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFindPeakHourTieBreak covers finding 3: ties must break toward the lowest
// hour so the result is deterministic regardless of map iteration order.
func TestFindPeakHourTieBreak(t *testing.T) {
	tests := []struct {
		name       string
		hourCounts map[string]int
		want       int
	}{
		{
			name:       "two-way tie picks lowest hour",
			hourCounts: map[string]int{"22": 50, "9": 50},
			want:       9,
		},
		{
			name:       "three-way tie picks lowest hour",
			hourCounts: map[string]int{"23": 7, "5": 7, "14": 7},
			want:       5,
		},
		{
			name:       "tie at zero count is not a peak",
			hourCounts: map[string]int{"5": 0, "3": 0},
			want:       0,
		},
		{
			name:       "higher count beats lower hour",
			hourCounts: map[string]int{"2": 10, "8": 99},
			want:       8,
		},
	}

	// Run each case many times: map iteration order is randomized per range, so
	// repetition exercises the nondeterminism the tie-break must defeat.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 200; i++ {
				if got := findPeakHour(tt.hourCounts); got != tt.want {
					t.Fatalf("findPeakHour() = %d, want %d (iteration %d)", got, tt.want, i)
				}
			}
		})
	}
}

// TestSupplementStaleDataNoDoubleCount covers finding 1: days already present
// in the cached weekly/total data must not be added again to the totals.
func TestSupplementStaleDataNoDoubleCount(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "projects", "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// One session file modified "now" (well after lastDate). 4096 bytes -> 2
	// estimated messages (1 per 2KB).
	f := filepath.Join(projDir, "s.jsonl")
	if err := os.WriteFile(f, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	today := time.Now().Format("2006-01-02")

	t.Run("already-counted day is not added to totals", func(t *testing.T) {
		st := &stats.Stats{
			TotalSessions: 10,
			TotalMessages: 20,
			// Today already represented in the cached weekly data.
			WeeklyActivity: []stats.DayActivity{
				{Date: today, SessionCount: 1, MessageCount: 2},
			},
		}
		supplementStaleData(st, dir, time.Now().AddDate(0, 0, -2))

		// The day was already present, so totals must be unchanged.
		if st.TotalSessions != 10 {
			t.Errorf("TotalSessions = %d, want 10 (no double count)", st.TotalSessions)
		}
		if st.TotalMessages != 20 {
			t.Errorf("TotalMessages = %d, want 20 (no double count)", st.TotalMessages)
		}
		// And the existing weekly counts must be preserved (guarded merge).
		if st.WeeklyActivity[0].SessionCount != 1 || st.WeeklyActivity[0].MessageCount != 2 {
			t.Errorf("weekly day mutated: %+v", st.WeeklyActivity[0])
		}
	})

	t.Run("new day is added to totals and weekly", func(t *testing.T) {
		st := &stats.Stats{
			TotalSessions: 10,
			TotalMessages: 20,
			// Today present but with zero counts -> supplement should fill it
			// and add it to the totals exactly once.
			WeeklyActivity: []stats.DayActivity{
				{Date: today, SessionCount: 0, MessageCount: 0},
			},
		}
		supplementStaleData(st, dir, time.Now().AddDate(0, 0, -2))

		if st.TotalSessions != 11 {
			t.Errorf("TotalSessions = %d, want 11", st.TotalSessions)
		}
		if st.TotalMessages != 22 {
			t.Errorf("TotalMessages = %d, want 22", st.TotalMessages)
		}
		if st.WeeklyActivity[0].SessionCount != 1 {
			t.Errorf("weekly SessionCount = %d, want 1", st.WeeklyActivity[0].SessionCount)
		}
		if st.WeeklyActivity[0].MessageCount != 2 {
			t.Errorf("weekly MessageCount = %d, want 2", st.WeeklyActivity[0].MessageCount)
		}
	})
}

// TestCachedProjectCwdScansOnce covers finding 4: a project's session files are
// scanned at most once per process, and later mutations of the files are not
// re-read.
func TestCachedProjectCwdScansOnce(t *testing.T) {
	resetProjectCwdCache()
	defer resetProjectCwdCache()

	dir := t.TempDir()
	f := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(f, []byte(`{"cwd":"/home/tom/work/myproj"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	slug := "home-tom-work-myproj"
	got := cachedProjectCwd(slug, []string{f})
	if got != "/home/tom/work/myproj" {
		t.Fatalf("cachedProjectCwd() = %q, want %q", got, "/home/tom/work/myproj")
	}

	// Overwrite the file with a different cwd. Because the slug is cached, the
	// next lookup must still return the original value (scanned once).
	if err := os.WriteFile(f, []byte(`{"cwd":"/some/other/path"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := cachedProjectCwd(slug, []string{f})
	if got2 != "/home/tom/work/myproj" {
		t.Errorf("cached lookup = %q, want memoized %q", got2, "/home/tom/work/myproj")
	}
}

// TestCachedProjectCwdCachesEmpty covers finding 4: projects with no recorded
// cwd cache the empty result so they are not re-scanned every recompute.
func TestCachedProjectCwdCachesEmpty(t *testing.T) {
	resetProjectCwdCache()
	defer resetProjectCwdCache()

	dir := t.TempDir()
	f := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	slug := "no-cwd-proj"
	if got := cachedProjectCwd(slug, []string{f}); got != "" {
		t.Fatalf("cachedProjectCwd() = %q, want empty", got)
	}

	// A later file gaining a cwd must not change the cached empty result.
	if err := os.WriteFile(f, []byte(`{"cwd":"/now/has/cwd"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cachedProjectCwd(slug, []string{f}); got != "" {
		t.Errorf("cached lookup = %q, want memoized empty", got)
	}
}

// resetProjectCwdCache clears the package-level slug->cwd memo so tests that
// reuse slugs do not observe each other's cached results.
func resetProjectCwdCache() {
	projectCwdMu.Lock()
	projectCwdBySlug = map[string]string{}
	projectCwdMu.Unlock()
}
