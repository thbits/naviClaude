package stats

// Stats holds computed statistics for display in the stats popup.
type Stats struct {
	TotalSessions    int
	TotalMessages    int
	ActiveCount      int
	AvgSessionsPerDay float64
	ProjectCounts    []ProjectCount
	WeeklyActivity   []DayActivity
	HourlyActivity   [24]int // message count per hour, only populated for "today" filter
	ModelUsage       []ModelUsageEntry
	LongestSession   LongestInfo
	PeakHour         int
	Filter           string // "all", "week", "today"
}

// ProjectCount is a project name with its session count.
type ProjectCount struct {
	Name  string
	Count int
}

// DayActivity is a single day's activity metrics.
type DayActivity struct {
	Date         string
	MessageCount int
	SessionCount int
}

// ModelUsageEntry is a model family with its usage count.
type ModelUsageEntry struct {
	Family string // "opus", "sonnet", "haiku"
	Count  int
}

// LongestInfo describes the longest session.
type LongestInfo struct {
	SessionID    string
	DurationMins int
	MessageCount int
}
