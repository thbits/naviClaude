package session

import "testing"

func TestProcessTreeSubtreeCPU(t *testing.T) {
	// Tree:
	//   10 -> 20, 30
	//   20 -> 40
	// CPU: 10=1.0, 20=2.0, 30=0.5, 40=5.0
	tree := &ProcessTree{
		children: map[int][]int{
			10: {20, 30},
			20: {40},
		},
		names: make(map[int]string),
		ppid:  make(map[int]int),
		cpu:   map[int]float64{10: 1.0, 20: 2.0, 30: 0.5, 40: 5.0},
		rss:   make(map[int]float64),
	}

	tests := []struct {
		name string
		pid  int
		want float64
	}{
		{"full subtree", 10, 8.5},
		{"partial subtree", 20, 7.0},
		{"leaf process", 40, 5.0},
		{"childless node", 30, 0.5},
		{"unknown pid", 999, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tree.SubtreeCPU(tt.pid)
			if got != tt.want {
				t.Errorf("SubtreeCPU(%d) = %v, want %v", tt.pid, got, tt.want)
			}
		})
	}
}

func TestProcessTreeSubtreeCPUHandlesCycles(t *testing.T) {
	// Defensive: a malformed tree with a cycle must not infinite-loop.
	tree := &ProcessTree{
		children: map[int][]int{
			10: {20},
			20: {10}, // cycle back to 10
		},
		names: make(map[int]string),
		ppid:  make(map[int]int),
		cpu:   map[int]float64{10: 1.0, 20: 2.0},
		rss:   make(map[int]float64),
	}

	got := tree.SubtreeCPU(10)
	if got != 3.0 {
		t.Errorf("SubtreeCPU(10) with cycle = %v, want 3.0", got)
	}
}
