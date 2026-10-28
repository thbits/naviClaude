package ui

import (
	"strings"
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
			[]string{"resume", "resume_jump", "fork_resume", "-", "detail", "-", "copy_id", "copy_path", "copy_resume"}},
		{"resume only", MenuCaps{CanResume: true},
			[]string{"resume", "resume_jump", "-", "detail", "-", "copy_id", "copy_path", "copy_resume"}},
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

func TestContextMenuSetAsUnread(t *testing.T) {
	m := NewContextMenu()
	has := func(s *session.Session) bool {
		for _, a := range actions(m.buildItems(s, MenuCaps{})) {
			if a == "mark_unread" {
				return true
			}
		}
		return false
	}
	tests := []struct {
		name string
		s    *session.Session
		want bool
	}{
		{"idle and seen", &session.Session{Status: session.StatusIdle, TmuxPaneID: "%1"}, true},
		{"already done", &session.Session{Status: session.StatusIdle, TmuxPaneID: "%1", Attention: "done"}, false},
		{"working", &session.Session{Status: session.StatusActive, TmuxPaneID: "%1"}, false},
		{"no pane", &session.Session{Status: session.StatusIdle}, false},
		{"closed", &session.Session{Status: session.StatusClosed, TmuxPaneID: "%1"}, false},
	}
	for _, tt := range tests {
		if got := has(tt.s); got != tt.want {
			t.Errorf("%s: has mark_unread = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestContextMenuFolderAndBranchItems(t *testing.T) {
	m := NewContextMenu()
	caps := MenuCaps{CanResume: true, Branch: "main"}

	closed := &session.Session{Status: session.StatusClosed, CWD: "/repo"}
	want := []string{"resume", "resume_jump", "-", "detail", "-", "open_editor", "open_folder", "-",
		"copy_id", "copy_path", "copy_branch", "copy_resume"}
	if got := actions(m.buildItems(closed, caps)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("closed items = %v, want %v", got, want)
	}

	live := &session.Session{Status: session.StatusActive, CWD: "/repo"}
	want = []string{"focus", "jump", "-", "detail", "-", "open_editor", "open_folder", "-",
		"copy_id", "copy_path", "copy_branch", "-", "kill"}
	if got := actions(m.buildItems(live, caps)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("live items = %v, want %v (no resume command for live sessions)", got, want)
	}
}
