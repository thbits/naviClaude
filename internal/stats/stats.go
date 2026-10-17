package stats

import "time"

// Stats holds usage statistics computed from session transcripts for one period.
type Stats struct {
	Filter string // "all", "week", "today"
	// Since is the start of the period; for "all" it is the oldest retained
	// transcript record, since Claude Code prunes old transcripts.
	Since       time.Time
	ActiveCount int
	Sessions    int // main sessions with any activity in the period
	Prompts     int // prompts the user sent (not tool results, commands or meta)
	Tokens      Tokens
	Projects    []ProjectCount
	Daily       []DayActivity // prompts per local day, oldest first
	Hourly      [24]int       // prompts per local hour of day
	BusiestHour int           // hour with the most prompts, -1 when there are none
	Models      []ModelUsageEntry
}

// Tokens sums API usage across assistant messages, each message counted once.
type Tokens struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Total is every token billed in or out, cache reads and writes included.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// ProjectCount is a project's activity in the period.
type ProjectCount struct {
	Name     string // shortest unambiguous path label, "~" for the home dir
	Sessions int
	Prompts  int
}

// DayActivity is one local day's prompt count.
type DayActivity struct {
	Date    string // YYYY-MM-DD
	Prompts int
}

// ModelUsageEntry is a model family's total tokens in the period.
type ModelUsageEntry struct {
	Family string // "opus", "sonnet", "haiku", "fable", "other"
	Tokens int64
}
