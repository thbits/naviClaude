package ui

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func actions(items []MenuItem) []string {
	var out []string
	for _, it := range items {
		if it.IsSep {
			out = append(out, "-")
			continue
		}
		out = append(out, it.Action)
	}
	return out
}

func TestContextMenuClosedCaps(t *testing.T) {
	closed := &session.Session{Status: session.StatusClosed}
	m := NewContextMenu()

	tests := []struct {
		name string
		caps MenuCaps
		want []string
	}{
		{"resume and fork", MenuCaps{CanResume: true, CanFork: true},
			[]string{"resume", "fork_resume", "-", "detail", "-", "copy_id", "copy_path"}},
		{"resume only", MenuCaps{CanResume: true},
			[]string{"resume", "-", "detail", "-", "copy_id", "copy_path"}},
		{"no lifecycle caps", MenuCaps{},
			[]string{"detail", "-", "copy_id", "copy_path"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actions(m.buildItems(closed, tt.caps))
			if len(got) != len(tt.want) {
				t.Fatalf("items = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("items = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestContextMenuLiveIgnoresCaps(t *testing.T) {
	live := &session.Session{Status: session.StatusActive}
	m := NewContextMenu()
	a := actions(m.buildItems(live, MenuCaps{}))
	b := actions(m.buildItems(live, MenuCaps{CanResume: true, CanFork: true}))
	if len(a) != len(b) || a[0] != "focus" {
		t.Errorf("live menu should not depend on caps: %v vs %v", a, b)
	}
}
