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

	t.Run("matching is substring, not loose subsequence", func(t *testing.T) {
		// "nvc" is a subsequence of "naviclaude" but not a substring; a loose
		// fuzzy matcher would surface it, which the user found unhelpful.
		got := filterSessions("nvc", []string{"naviclaude"})
		if len(got) != 0 {
			t.Errorf("got %v, want empty (nvc is not a substring of naviclaude)", got)
		}
	})

	t.Run("matching is case-insensitive", func(t *testing.T) {
		got := filterSessions("WORK", sessions)
		if !contains(got, "work") {
			t.Errorf("got %v, want to contain work", got)
		}
	})

	t.Run("substring matches preserve input (recency) order", func(t *testing.T) {
		got := filterSessions("o", []string{"work", "wonder", "scratch", "convoy"})
		want := []string{"work", "wonder", "convoy"} // all contain 'o', original order
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestSessionPickerRows(t *testing.T) {
	t.Run("create-new is at the bottom when sessions match", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "naviclaude"}, "work")
		m.input.SetValue("navi")
		m.runFilter()
		rows := m.rows()
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 (naviclaude + create-new)", len(rows))
		}
		if rows[0].name != "naviclaude" || rows[0].isNew {
			t.Errorf("rows[0] = %+v, want naviclaude (existing)", rows[0])
		}
		if !rows[len(rows)-1].isNew {
			t.Errorf("last row should be the create-new row, got %+v", rows[len(rows)-1])
		}
	})

	t.Run("create-new floats to the top when nothing matches", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "naviclaude"}, "work")
		m.input.SetValue("brandnew")
		m.runFilter()
		rows := m.rows()
		if len(rows) != 1 || !rows[0].isNew || rows[0].name != "brandnew" {
			t.Fatalf("rows = %+v, want only the create-new row at top", rows)
		}
	})

	t.Run("typing resets the cursor to the top row", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "scratch", "naviclaude"}, "naviclaude")
		// Show pre-highlights the current session (cursor moved off row 0).
		m.input.SetValue("w")
		m.runFilter()
		if m.cursor != 0 {
			t.Errorf("cursor = %d after typing, want 0 (top row)", m.cursor)
		}
	})

	t.Run("no substring match: create-new on top, loose fuzzy matches below", func(t *testing.T) {
		m := NewSessionPicker()
		m.Show([]string{"work", "naviclaude"}, "work")
		m.input.SetValue("nvc") // subsequence of naviclaude, but not a substring
		m.runFilter()
		rows := m.rows()
		if len(rows) != 2 {
			t.Fatalf("rows = %+v, want 2 (create-new + loose naviclaude)", rows)
		}
		if !rows[0].isNew || rows[0].name != "nvc" {
			t.Errorf("rows[0] = %+v, want create-new 'nvc' on top", rows[0])
		}
		if rows[1].name != "naviclaude" || rows[1].isNew || !rows[1].loose {
			t.Errorf("rows[1] = %+v, want loose (fuzzy) naviclaude below", rows[1])
		}
	})

	t.Run("loose fuzzy matches are hidden when a substring matches", func(t *testing.T) {
		// "na" is a substring of "naviclaude" and a subsequence of "nota"
		// (n..a) but not a substring of it; with a real substring hit present,
		// the loose-only "nota" must not appear.
		m := NewSessionPicker()
		m.Show([]string{"naviclaude", "nota"}, "naviclaude")
		m.input.SetValue("na")
		m.runFilter()
		for _, r := range m.rows() {
			if r.loose || r.name == "nota" {
				t.Errorf("no loose rows expected when a substring matches; got %+v", m.rows())
				break
			}
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
