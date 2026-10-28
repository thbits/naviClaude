package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// searchModel is clickModel in search mode with a query matching only the
// closed session c1, which the search cursor sits on.
func searchModel(t *testing.T) *Model {
	t.Helper()
	m, _ := clickModel(t)
	m.registry = harness.NewRegistry(claude.New(claude.Options{}))
	m.resumePicker = ui.NewSessionPicker()
	m.contextMenu = ui.NewContextMenu()
	for _, s := range m.allSessions {
		if s.ID == "c1" {
			s.ProjectName = "legacy" // search matches project names
		}
	}
	key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "legacy" {
		key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.mode != ModeSearch {
		t.Fatalf("precondition: mode = %v, want search", m.mode)
	}
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.ID != "c1" {
		t.Fatalf("precondition: search cursor = %+v, want closed session c1", sel)
	}
	return m
}

func key(m *Model, k tea.KeyMsg) tea.Cmd {
	next, cmd := m.handleKey(k)
	*m = next.(Model)
	return cmd
}

func TestSearchEnterOnClosedOpensResumePicker(t *testing.T) {
	m := searchModel(t)
	key(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != ModeResumePicker || !m.resumePicker.IsVisible() {
		t.Fatalf("mode = %v picker = %v, want the resume picker", m.mode, m.resumePicker.IsVisible())
	}
	if m.pendingResumeSession == nil || m.pendingResumeSession.ID != "c1" || m.pendingResumeJump {
		t.Errorf("pending = %+v jump = %v, want c1 without jump", m.pendingResumeSession, m.pendingResumeJump)
	}
	if !m.search.IsActive() {
		t.Error("search closed under the picker; Esc could not return to it")
	}

	// Esc returns to the search with its results intact.
	key(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeSearch || !m.search.IsActive() {
		t.Fatalf("mode = %v search = %v, want back in search", m.mode, m.search.IsActive())
	}
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.ID != "c1" {
		t.Errorf("selection = %+v, want c1 still highlighted", sel)
	}
}

func TestSearchCtrlFOnClosedResumesWithJump(t *testing.T) {
	m := searchModel(t)
	key(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.mode != ModeResumePicker || !m.pendingResumeJump {
		t.Fatalf("mode = %v jump = %v, want picker with jump", m.mode, m.pendingResumeJump)
	}
}

func TestSearchResumePickerConfirmEndsSearch(t *testing.T) {
	m := searchModel(t)
	key(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.resumePicker.Show([]string{"alpha"}, "")
	cmd := key(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirm returned no resume command")
	}
	if m.search.IsActive() || m.mode != ModeList {
		t.Errorf("search = %v mode = %v, want search ended in list mode", m.search.IsActive(), m.mode)
	}
}

func TestSearchRightClickOpensMenu(t *testing.T) {
	m := searchModel(t)
	y := rowOf(t, m, "c1")
	press(m, tea.MouseButtonRight, xList, y)
	if m.mode != ModeContextMenu || !m.contextMenu.IsVisible() {
		t.Fatalf("mode = %v menu = %v, want the context menu", m.mode, m.contextMenu.IsVisible())
	}
	if got := m.contextMenu.Session(); got == nil || got.ID != "c1" {
		t.Errorf("menu session = %+v, want c1", got)
	}

	// Esc returns to the search.
	key(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeSearch || !m.search.IsActive() {
		t.Fatalf("mode = %v search = %v, want back in search", m.mode, m.search.IsActive())
	}

	// Running an item (Resume) leaves the search for the picker.
	press(m, tea.MouseButtonRight, xList, y)
	key(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.search.IsActive() || m.mode != ModeResumePicker {
		t.Errorf("search = %v mode = %v, want search ended and the resume picker open", m.search.IsActive(), m.mode)
	}
}

// With renumber-windows a window closing shifts the new pane's target before
// the agent is detected; the pane id must still match it.
func TestPendingPlaceholderMatchesByPaneID(t *testing.T) {
	m := newFocusModel(0, nil)
	m.enterNewSessionPassthrough(&session.Session{
		TmuxSession: "s", TmuxTarget: "s:3.1", TmuxPaneID: "%9", Status: session.StatusActive,
	})
	other := &session.Session{ID: "x", TmuxSession: "s", TmuxTarget: "s:3.1", TmuxPaneID: "%4", Status: session.StatusActive}
	got := m.mergePendingPlaceholder([]*session.Session{other})
	if m.pendingNewTarget == "" || len(got) != 2 {
		t.Fatalf("a different pane at the old target cleared the placeholder (got %d sessions)", len(got))
	}

	detected := &session.Session{ID: "new", TmuxSession: "s", TmuxTarget: "s:2.1", TmuxPaneID: "%9", Status: session.StatusActive}
	active := m.mergePendingPlaceholder([]*session.Session{other, detected})
	if m.pendingNewTarget != "" || len(active) != 2 {
		t.Fatalf("pending = %q, %d sessions; want the detected pane to replace the placeholder", m.pendingNewTarget, len(active))
	}
	m.sessions = active
	m.sidebar.SetSessions(active)
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.ID != "new" {
		t.Errorf("selection = %+v, want the new session after renumbering", sel)
	}
}

func TestNewTmuxSessionFocusesIt(t *testing.T) {
	m := newFocusModel(0, []*session.Session{
		{ID: "old", TmuxSession: "other", TmuxTarget: "other:1.1", Status: session.StatusActive},
	})
	next, _ := m.Update(newTmuxSessionMsg{tmuxTarget: "proj:1.1", paneID: "%7", tmuxSession: "proj", cwd: "/tmp/proj", harness: "claude"})
	m = next.(Model)
	if m.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough on the new session", m.mode)
	}
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.TmuxTarget != "proj:1.1" || sel.TmuxPaneID != "%7" {
		t.Errorf("selection = %+v, want the new proj:1.1 pane", sel)
	}
}

func TestSearchTypingSyncsPreviewToTopHit(t *testing.T) {
	m := searchModel(t)
	if !m.selectionPending {
		t.Error("typing moved the highlight to c1 but scheduled no preview sync")
	}
}

func TestClosedPreviewDropsStaleSession(t *testing.T) {
	m := searchModel(t)
	m.preview.SetContent("current")
	next, _ := m.Update(closedPreviewMsg{key: "a1", content: "stale"})
	m = ptr(next.(Model))
	if strings.Contains(m.preview.View(), "stale") {
		t.Error("a preview for a1 overwrote the selected c1's preview")
	}
	next, _ = m.Update(closedPreviewMsg{key: "c1", content: "fresh"})
	m = ptr(next.(Model))
	if !strings.Contains(m.preview.View(), "fresh") {
		t.Error("the selected session's preview was dropped")
	}
}

func ptr(m Model) *Model { return &m }
