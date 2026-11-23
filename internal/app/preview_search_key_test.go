package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/ui"
)

// previewSearchModel is a list-mode model previewing a-idle with three "alpha" hits.
func previewSearchModel(t *testing.T) *Model {
	t.Helper()
	m := statusModel(t, statusSessions())
	m.sidebar.SelectByID("a-idle")
	m.afterSelectionChange()
	m.preview.SetContent("alpha one\nbeta\nalpha two\ngamma alpha")
	return m
}

func matchCount(m *Model) [2]int {
	cur, total := m.preview.SearchCount()
	return [2]int{cur, total}
}

func TestPreviewSearchKeyTypesIntoQueryNotTheList(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S")
	if m.mode != ModePreviewSearch || !m.preview.SearchTyping() {
		t.Fatalf("mode = %v, typing = %v; want the query input open", m.mode, m.preview.SearchTyping())
	}
	// Letters that are list actions (filter, quit, kill...) must only type.
	typeKeys(t, m, "a", "l", "p", "h", "a")
	if m.mode != ModePreviewSearch || m.sidebar.StatusFilter() != ui.FilterNone || selectedID(m) != "a-idle" {
		t.Fatalf("typing ran list actions: mode = %v, filter = %q, selected = %q", m.mode, m.sidebar.StatusFilter(), selectedID(m))
	}
	if got := matchCount(m); got != [2]int{1, 3} {
		t.Errorf("count = %v, want 1/3", got)
	}
	if header := ansi.Strip(m.preview.View()); !strings.Contains(header, "find: alpha") || !strings.Contains(header, "1/3") {
		t.Errorf("preview should show the query and counter:\n%s", header)
	}
	if line := statusText(m); !strings.Contains(line, "keep matches") || !strings.Contains(line, "Esc clear") {
		t.Errorf("status hints = %q, want the search input's keys", line)
	}
}

func TestPreviewSearchEnterKeepsMatchesForNAndShiftN(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a", "l", "p", "h", "a")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != ModeList || m.preview.SearchTyping() || !m.preview.HasSearch() {
		t.Fatalf("after Enter: mode = %v, typing = %v, has search = %v", m.mode, m.preview.SearchTyping(), m.preview.HasSearch())
	}
	if line := statusText(m); !strings.Contains(line, "n next match") || !strings.Contains(line, "N prev match") || !strings.Contains(line, "? more") {
		t.Errorf("status hints = %q, want the match keys and help", line)
	}

	var seen [][2]int
	for _, k := range []string{"n", "n", "n", "N"} {
		typeKeys(t, m, k)
		seen = append(seen, matchCount(m))
	}
	want := [][2]int{{2, 3}, {3, 3}, {1, 3}, {3, 3}}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("n n n N visited %v, want %v", seen, want)
		}
	}
	// n and N must not have started the new-session flows they otherwise mean.
	if m.mode != ModeList || m.dirPicker.IsVisible() {
		t.Errorf("mode = %v, dir picker visible = %v; n / N leaked to the list", m.mode, m.dirPicker.IsVisible())
	}
}

func TestPreviewSearchArrowsStepWhileTyping(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a", "l", "p", "h", "a", "down", "down", "up")
	if got := matchCount(m); got != [2]int{2, 3} {
		t.Errorf("count = %v, want 2/3", got)
	}
	if m.mode != ModePreviewSearch {
		t.Errorf("mode = %v, want the input still open", m.mode)
	}
}

func TestPreviewSearchEscClearsFromInputAndFromList(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a", "esc")
	if m.mode != ModeList || m.preview.HasSearch() {
		t.Fatalf("Esc in the input: mode = %v, has search = %v", m.mode, m.preview.HasSearch())
	}

	typeKeys(t, m, "S", "a")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	typeKeys(t, m, "esc")
	if m.preview.HasSearch() {
		t.Fatal("Esc in the list should clear the kept matches")
	}
	if line := statusText(m); strings.Contains(line, "next match") {
		t.Errorf("status hints = %q, want the list's own hints back", line)
	}
	if view := ansi.Strip(m.preview.View()); strings.Contains(view, "find:") {
		t.Errorf("the search bar outlived the search:\n%s", view)
	}
}

// Esc peels the preview search first and the status filter second.
func TestEscClearsPreviewSearchBeforeStatusFilter(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "i") // a-idle stays selected under the idle filter
	typeKeys(t, m, "S", "a")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	typeKeys(t, m, "esc")
	if m.preview.HasSearch() || m.sidebar.StatusFilter() != ui.FilterIdle {
		t.Fatalf("first Esc: has search = %v, filter = %q; want only the search cleared", m.preview.HasSearch(), m.sidebar.StatusFilter())
	}
	typeKeys(t, m, "esc")
	if m.sidebar.StatusFilter() != ui.FilterNone {
		t.Errorf("second Esc: filter = %q, want it cleared", m.sidebar.StatusFilter())
	}
}

func TestPreviewSearchEnterOnEmptyQueryEndsIt(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != ModeList || m.preview.HasSearch() {
		t.Errorf("mode = %v, has search = %v; want nothing left", m.mode, m.preview.HasSearch())
	}
	if line := statusText(m); strings.Contains(line, "next match") {
		t.Errorf("status hints = %q, want the list's own hints", line)
	}
}

// The matches belong to the content they were found in: another session's
// preview starts clean, and n / N are the list's keys again.
func TestPreviewSearchEndsWhenSelectionChanges(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	typeKeys(t, m, "j")
	send(t, m, selectionSettledMsg{gen: m.selectionGen})
	if m.preview.HasSearch() {
		t.Error("the search should end with the selection it was run on")
	}
	if line := statusText(m); strings.Contains(line, "next match") {
		t.Errorf("status hints = %q, want the list's own hints", line)
	}
}

func TestPreviewSearchEndsOnPassthrough(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.setFocus(ModePassthrough)
	if m.preview.HasSearch() {
		t.Error("entering passthrough should clear the search")
	}
}

// A click that moves focus mid-query keeps what was typed, as Enter does.
func TestPreviewSearchFocusChangeCommitsQuery(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a")
	m.setFocus(ModeList)
	if m.preview.SearchTyping() || !m.preview.HasSearch() || m.mode != ModeList {
		t.Errorf("typing = %v, has search = %v, mode = %v", m.preview.SearchTyping(), m.preview.HasSearch(), m.mode)
	}
	if line := statusText(m); !strings.Contains(line, "next match") {
		t.Errorf("status hints = %q, want the match keys", line)
	}
}

func TestPreviewSearchInputSurvivesRefreshMovingTheCursor(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a", "l")
	// A refresh can move the cursor (the selected session exited); keys must stay search text.
	m.sidebar.SelectByID("b-wait")
	m.afterSelectionChange()
	typeKeys(t, m, "q", "K")
	if m.mode != ModePreviewSearch || !m.preview.SearchTyping() {
		t.Fatalf("mode = %v, typing = %v; want the query input still open", m.mode, m.preview.SearchTyping())
	}
}

func TestContextMenuFocusEndsPreviewSearch(t *testing.T) {
	m := previewSearchModel(t)
	typeKeys(t, m, "S", "a", "l", "p", "h", "a")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	sel := m.sidebar.SelectedSession()
	next, _ := m.executeContextAction("focus", sel)
	got := next.(Model)
	if got.mode != ModePassthrough || got.preview.HasSearch() {
		t.Errorf("mode = %v, has search = %v; want passthrough with the search cleared", got.mode, got.preview.HasSearch())
	}
}
