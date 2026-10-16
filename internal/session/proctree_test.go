package session

import (
	"testing"
)

func TestProcessTreeStats(t *testing.T) {
	tree := &ProcessTree{
		children: make(map[int][]int),
		names:    make(map[int]string),
		ppid:     make(map[int]int),
		cpu:      map[int]float64{100: 12.5},
		rss:      map[int]float64{100: 51200}, // 50 MB in KB
	}

	cpu, mem := tree.Stats(100)
	if cpu != 12.5 {
		t.Errorf("cpu = %f, want 12.5", cpu)
	}
	if mem != 50.0 {
		t.Errorf("mem = %f, want 50.0", mem)
	}

	// Unknown PID returns zeros.
	cpu, mem = tree.Stats(999)
	if cpu != 0 || mem != 0 {
		t.Errorf("unknown PID: cpu=%f, mem=%f, want 0, 0", cpu, mem)
	}
}

func TestProcessTreeIsAncestorOf(t *testing.T) {
	tree := &ProcessTree{
		children: make(map[int][]int),
		names:    make(map[int]string),
		ppid: map[int]int{
			10: 1,
			20: 10,
			30: 20,
			40: 30,
		},
		cpu: make(map[int]float64),
		rss: make(map[int]float64),
	}

	tests := []struct {
		name     string
		ancestor int
		desc     int
		want     bool
	}{
		{"direct parent", 20, 30, true},
		{"grandparent", 10, 30, true},
		{"great-grandparent", 10, 40, true},
		{"not ancestor", 30, 10, false},
		{"same PID", 20, 20, false},
		{"unknown PID", 999, 30, false},
		{"root process", 1, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tree.IsAncestorOf(tt.ancestor, tt.desc)
			if got != tt.want {
				t.Errorf("isAncestorOf(%d, %d) = %v, want %v", tt.ancestor, tt.desc, got, tt.want)
			}
		})
	}
}

func TestProcessTreeFindMatchingDescendant(t *testing.T) {
	tree := &ProcessTree{
		children: map[int][]int{
			1:  {10},
			10: {20, 30},
			20: {40},
		},
		names: map[int]string{
			10: "bash",
			20: "node",
			30: "claude",
			40: "python",
		},
		ppid: make(map[int]int),
		cpu:  make(map[int]float64),
		rss:  make(map[int]float64),
	}

	matchClaude := func(name string) bool { return name == "claude" }

	t.Run("finds direct child", func(t *testing.T) {
		got := tree.FindMatchingDescendant(10, matchClaude, 3)
		if got != 30 {
			t.Errorf("got PID %d, want 30", got)
		}
	})

	t.Run("no match returns 0", func(t *testing.T) {
		matchVim := func(name string) bool { return name == "vim" }
		got := tree.FindMatchingDescendant(1, matchVim, 10)
		if got != 0 {
			t.Errorf("got PID %d, want 0", got)
		}
	})

	t.Run("maxDepth 0 returns 0", func(t *testing.T) {
		got := tree.FindMatchingDescendant(10, matchClaude, 0)
		if got != 0 {
			t.Errorf("got PID %d, want 0", got)
		}
	})

	t.Run("depth limited search", func(t *testing.T) {
		// claude is at depth 1 from PID 10, so maxDepth=1 should find it.
		got := tree.FindMatchingDescendant(10, matchClaude, 1)
		if got != 30 {
			t.Errorf("got PID %d, want 30", got)
		}
	})
}

func TestSessionStatus(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   string
	}{
		{StatusActive, "Active"},
		{StatusIdle, "Idle"},
		{StatusWaiting, "Waiting"},
		{StatusClosed, "Closed"},
		{SessionStatus(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
