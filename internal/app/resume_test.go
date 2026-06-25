package app

import (
	"testing"

	"github.com/thbits/naviClaude/internal/tmux"
)

func TestSessionNamesByRecency(t *testing.T) {
	t.Run("orders by most-recent activity first", func(t *testing.T) {
		got := sessionNamesByRecency([]tmux.SessionInfo{
			{Name: "old", Activity: 100},
			{Name: "newest", Activity: 300},
			{Name: "mid", Activity: 200},
		})
		want := []string{"newest", "mid", "old"}
		if !equalStringSlice(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("equal activity preserves input order (stable)", func(t *testing.T) {
		got := sessionNamesByRecency([]tmux.SessionInfo{
			{Name: "a", Activity: 100},
			{Name: "b", Activity: 100},
			{Name: "c", Activity: 100},
		})
		want := []string{"a", "b", "c"}
		if !equalStringSlice(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("empty input yields empty slice", func(t *testing.T) {
		if got := sessionNamesByRecency(nil); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
