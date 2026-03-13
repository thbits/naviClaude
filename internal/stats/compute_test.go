package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGroupModelUsage(t *testing.T) {
	tests := []struct {
		name      string
		usage     map[string]modelUsageRaw
		wantCount int
		wantFirst string // family name of highest-count entry
	}{
		{
			name:      "nil map",
			usage:     nil,
			wantCount: 0,
		},
		{
			name:      "empty map",
			usage:     map[string]modelUsageRaw{},
			wantCount: 0,
		},
		{
			name: "single opus model",
			usage: map[string]modelUsageRaw{
				"claude-opus-4-6": {InputTokens: 1000, OutputTokens: 500},
			},
			wantCount: 1,
			wantFirst: "opus",
		},
		{
			name: "multiple models same family aggregated",
			usage: map[string]modelUsageRaw{
				"claude-sonnet-4-6":      {InputTokens: 100, OutputTokens: 200},
				"claude-sonnet-4-5":      {InputTokens: 300, OutputTokens: 400},
			},
			wantCount: 1,
			wantFirst: "sonnet",
		},
		{
			name: "all three families sorted by count desc",
			usage: map[string]modelUsageRaw{
				"claude-haiku-4-5":  {InputTokens: 10, OutputTokens: 10},
				"claude-opus-4-6":   {InputTokens: 5000, OutputTokens: 5000},
				"claude-sonnet-4-6": {InputTokens: 100, OutputTokens: 100},
			},
			wantCount: 3,
			wantFirst: "opus",
		},
		{
			name: "unknown model ignored",
			usage: map[string]modelUsageRaw{
				"gpt-4": {InputTokens: 999, OutputTokens: 999},
			},
			wantCount: 0,
		},
		{
			name: "case insensitive matching",
			usage: map[string]modelUsageRaw{
				"Claude-OPUS-4-6": {InputTokens: 100, OutputTokens: 100},
			},
			wantCount: 1,
			wantFirst: "opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := groupModelUsage(tt.usage)
			if len(result) != tt.wantCount {
				t.Fatalf("got %d entries, want %d", len(result), tt.wantCount)
			}
			if tt.wantCount > 0 && result[0].Family != tt.wantFirst {
				t.Errorf("first family = %q, want %q", result[0].Family, tt.wantFirst)
			}
		})
	}
}

func TestFindPeakHour(t *testing.T) {
	tests := []struct {
		name       string
		hourCounts map[string]int
		want       int
	}{
		{
			name:       "nil map returns 0",
			hourCounts: nil,
			want:       0,
		},
		{
			name:       "empty map returns 0",
			hourCounts: map[string]int{},
			want:       0,
		},
		{
			name:       "single hour",
			hourCounts: map[string]int{"14": 42},
			want:       14,
		},
		{
			name:       "multiple hours returns highest",
			hourCounts: map[string]int{"9": 10, "14": 50, "22": 30},
			want:       14,
		},
		{
			name:       "hour zero",
			hourCounts: map[string]int{"0": 100},
			want:       0,
		},
		{
			name:       "hour 23",
			hourCounts: map[string]int{"23": 5, "1": 3},
			want:       23,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findPeakHour(tt.hourCounts)
			if got != tt.want {
				t.Errorf("findPeakHour() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFilterDailyActivity(t *testing.T) {
	today := time.Now().Format("2006-01-02")

	daily := []dailyActivity{
		{Date: today, MessageCount: 10, SessionCount: 2},
	}

	t.Run("today filter returns single day", func(t *testing.T) {
		result := filterDailyActivity(daily, "today")
		if len(result) != 1 {
			t.Fatalf("got %d days, want 1", len(result))
		}
		if result[0].Date != today {
			t.Errorf("date = %q, want %q", result[0].Date, today)
		}
		if result[0].MessageCount != 10 {
			t.Errorf("message count = %d, want 10", result[0].MessageCount)
		}
	})

	t.Run("week filter returns 7 days", func(t *testing.T) {
		result := filterDailyActivity(daily, "week")
		if len(result) != 7 {
			t.Fatalf("got %d days, want 7", len(result))
		}
	})

	t.Run("all filter returns 30 days", func(t *testing.T) {
		result := filterDailyActivity(daily, "all")
		if len(result) != 30 {
			t.Fatalf("got %d days, want 30", len(result))
		}
	})

	t.Run("today with no matching data returns placeholder", func(t *testing.T) {
		result := filterDailyActivity(nil, "today")
		if len(result) != 1 {
			t.Fatalf("got %d days, want 1", len(result))
		}
		if result[0].MessageCount != 0 {
			t.Errorf("expected zero message count for missing day")
		}
	})

	t.Run("week with empty data fills all 7 days", func(t *testing.T) {
		result := filterDailyActivity(nil, "week")
		if len(result) != 7 {
			t.Fatalf("got %d days, want 7", len(result))
		}
		for _, d := range result {
			if d.MessageCount != 0 || d.SessionCount != 0 {
				t.Errorf("expected zero counts for day %s", d.Date)
			}
		}
	})
}

func TestCompute(t *testing.T) {
	t.Run("with valid stats-cache.json", func(t *testing.T) {
		dir := t.TempDir()
		cache := claudeStatsCache{
			TotalSessions:    42,
			TotalMessages:    100,
			FirstSessionDate: "2025-01-01",
			ModelUsage: map[string]modelUsageRaw{
				"claude-opus-4-6": {InputTokens: 500, OutputTokens: 500},
			},
			HourCounts: map[string]int{"14": 20},
			LongestSession: longestSessionRaw{
				SessionID:    "abc-123",
				Duration:     3600000,
				MessageCount: 50,
			},
		}
		data, _ := json.Marshal(cache)
		os.WriteFile(filepath.Join(dir, "stats-cache.json"), data, 0o644)
		os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

		st, err := Compute(dir, 3, "all")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.TotalSessions != 42 {
			t.Errorf("TotalSessions = %d, want 42", st.TotalSessions)
		}
		if st.TotalMessages != 100 {
			t.Errorf("TotalMessages = %d, want 100", st.TotalMessages)
		}
		if st.ActiveCount != 3 {
			t.Errorf("ActiveCount = %d, want 3", st.ActiveCount)
		}
		if st.Filter != "all" {
			t.Errorf("Filter = %q, want %q", st.Filter, "all")
		}
		if st.PeakHour != 14 {
			t.Errorf("PeakHour = %d, want 14", st.PeakHour)
		}
		if st.LongestSession.DurationMins != 60 {
			t.Errorf("LongestSession.DurationMins = %d, want 60", st.LongestSession.DurationMins)
		}
		if len(st.ModelUsage) != 1 || st.ModelUsage[0].Family != "opus" {
			t.Errorf("ModelUsage unexpected: %+v", st.ModelUsage)
		}
		if st.AvgSessionsPerDay <= 0 {
			t.Error("expected positive AvgSessionsPerDay")
		}
	})

	t.Run("missing stats-cache.json returns empty stats", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

		st, err := Compute(dir, 0, "week")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.TotalSessions != 0 {
			t.Errorf("TotalSessions = %d, want 0", st.TotalSessions)
		}
	})

	t.Run("invalid JSON in stats-cache.json is handled", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "stats-cache.json"), []byte("not json"), 0o644)
		os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

		st, err := Compute(dir, 0, "all")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.TotalSessions != 0 {
			t.Errorf("TotalSessions = %d, want 0", st.TotalSessions)
		}
	})
}

func TestCountSessionFiles(t *testing.T) {
	t.Run("counts jsonl files", func(t *testing.T) {
		dir := t.TempDir()
		projDir := filepath.Join(dir, "projects", "myproject")
		os.MkdirAll(projDir, 0o755)
		os.WriteFile(filepath.Join(projDir, "sess1.jsonl"), []byte("{}"), 0o644)
		os.WriteFile(filepath.Join(projDir, "sess2.jsonl"), []byte("{}"), 0o644)
		os.WriteFile(filepath.Join(projDir, "readme.txt"), []byte("hi"), 0o644)

		got := CountSessionFiles(dir)
		if got != 2 {
			t.Errorf("CountSessionFiles = %d, want 2", got)
		}
	})

	t.Run("empty projects dir returns 0", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

		got := CountSessionFiles(dir)
		if got != 0 {
			t.Errorf("CountSessionFiles = %d, want 0", got)
		}
	})

	t.Run("nonexistent dir returns 0", func(t *testing.T) {
		got := CountSessionFiles("/nonexistent/path/that/does/not/exist")
		if got != 0 {
			t.Errorf("CountSessionFiles = %d, want 0", got)
		}
	})
}

func TestScanProjectCounts(t *testing.T) {
	t.Run("counts projects with no filter", func(t *testing.T) {
		dir := t.TempDir()
		proj1 := filepath.Join(dir, "projects", "Users-tom-myapp")
		proj2 := filepath.Join(dir, "projects", "Users-tom-other")
		os.MkdirAll(proj1, 0o755)
		os.MkdirAll(proj2, 0o755)
		os.WriteFile(filepath.Join(proj1, "a.jsonl"), []byte("{}"), 0o644)
		os.WriteFile(filepath.Join(proj1, "b.jsonl"), []byte("{}"), 0o644)
		os.WriteFile(filepath.Join(proj2, "c.jsonl"), []byte("{}"), 0o644)

		counts := scanProjectCounts(dir, "all")
		if len(counts) != 2 {
			t.Fatalf("got %d project counts, want 2", len(counts))
		}
		// First should be the one with more files.
		if counts[0].Count != 2 {
			t.Errorf("top project count = %d, want 2", counts[0].Count)
		}
	})

	t.Run("empty projects dir returns nil", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

		counts := scanProjectCounts(dir, "all")
		if len(counts) != 0 {
			t.Errorf("expected 0 counts, got %d", len(counts))
		}
	})

	t.Run("nonexistent dir returns nil", func(t *testing.T) {
		counts := scanProjectCounts("/nonexistent", "all")
		if counts != nil {
			t.Errorf("expected nil, got %v", counts)
		}
	})

	t.Run("limits to top 10", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < 15; i++ {
			projDir := filepath.Join(dir, "projects", "proj"+string(rune('a'+i)))
			os.MkdirAll(projDir, 0o755)
			os.WriteFile(filepath.Join(projDir, "s.jsonl"), []byte("{}"), 0o644)
		}
		counts := scanProjectCounts(dir, "all")
		if len(counts) > 10 {
			t.Errorf("expected at most 10, got %d", len(counts))
		}
	})
}
