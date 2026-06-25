package ui

import "testing"

func TestFilterSessions(t *testing.T) {
	sessions := []string{"work", "scratch", "wonder", "naviclaude"}

	t.Run("empty query returns all unchanged", func(t *testing.T) {
		got := filterSessions("  ", sessions)
		if !equalStrings(got, sessions) {
			t.Errorf("got %v, want unchanged %v", got, sessions)
		}
	})

	t.Run("fuzzy matches a subset", func(t *testing.T) {
		got := filterSessions("wo", sessions)
		if !contains(got, "work") || !contains(got, "wonder") {
			t.Errorf("got %v, want to contain work and wonder", got)
		}
		if contains(got, "scratch") {
			t.Errorf("got %v, should not contain scratch", got)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := filterSessions("zzzzz", sessions)
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestNewSessionName(t *testing.T) {
	sessions := []string{"work", "scratch"}

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"empty query offers nothing", "", ""},
		{"whitespace query offers nothing", "   ", ""},
		{"exact match offers nothing", "work", ""},
		{"non-matching name is offered", "newproj", "newproj"},
		{"name is trimmed", "  spaced  ", "spaced"},
		{"partial of existing is still a new name", "wo", "wo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newSessionName(tt.query, sessions); got != tt.want {
				t.Errorf("newSessionName(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestSessionPickerSelected(t *testing.T) {
	t.Run("preselects the current session", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "scratch", "naviclaude"}, "scratch")
		name, isNew := m.Selected()
		if name != "scratch" || isNew {
			t.Errorf("Selected() = (%q, %v), want (scratch, false)", name, isNew)
		}
	})

	t.Run("missing current falls back to first row", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "scratch"}, "gone")
		name, isNew := m.Selected()
		if name != "work" || isNew {
			t.Errorf("Selected() = (%q, %v), want (work, false)", name, isNew)
		}
	})

	t.Run("create-new row reports isNew with the typed name", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "scratch"}, "work")
		m.input.SetValue("newproj")
		m.runFilter()
		// Only the create-new row should be present (no fuzzy match for newproj).
		name, isNew := m.Selected()
		if name != "newproj" || !isNew {
			t.Errorf("Selected() = (%q, %v), want (newproj, true)", name, isNew)
		}
	})

	t.Run("empty picker selects nothing", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show(nil, "")
		name, isNew := m.Selected()
		if name != "" || isNew {
			t.Errorf("Selected() = (%q, %v), want (\"\", false)", name, isNew)
		}
	})
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
