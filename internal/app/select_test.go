package app

import (
	"testing"

	"github.com/thbits/naviClaude/internal/ui"
)

type fakeHandoff struct{ pending map[string]string }

func (f *fakeHandoff) TakeSelect(pane string) string {
	v := f.pending[pane]
	delete(f.pending, pane)
	return v
}

func TestSelectByPaneIDOverridesFocusLastAndExpandsGroup(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SelectByID("a-wait")
	for i, it := range m.sidebar.FlatItems() {
		if it.IsGroup {
			m.sidebar.SetCursor(i)
			if m.sidebar.SelectedGroupName() == "bb" {
				break
			}
		}
	}
	m.sidebar.ToggleGroupAtCursor() // bb collapsed, hiding b-done
	if listedHas(m, "b-done") {
		t.Fatal("precondition: bb should be collapsed")
	}
	*m = m.WithSelect("%6")
	if !m.restoredLastSession {
		t.Error("--select must override focus_last_session")
	}
	m.applyPendingSelect()
	if got := selectedID(m); got != "b-done" {
		t.Errorf("selected %q, want b-done", got)
	}
	if m.pendingSelect != "" {
		t.Error("request should be spent once applied")
	}
}

func TestSelectBySessionIDClearsHidingFilter(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SetStatusFilter(ui.FilterWaiting)
	m.requestSelect("a-idle")
	m.applyPendingSelect()
	if got := selectedID(m); got != "a-idle" {
		t.Errorf("selected %q, want a-idle", got)
	}
	if m.sidebar.StatusFilter() != ui.FilterNone {
		t.Error("a filter that hides the session must be cleared")
	}
}

func TestSelectKeepsFilterWhenSessionIsShown(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SetStatusFilter(ui.FilterWaiting)
	m.requestSelect("%5")
	m.applyPendingSelect()
	if selectedID(m) != "b-wait" || m.sidebar.StatusFilter() != ui.FilterWaiting {
		t.Errorf("selected %q filter %v", selectedID(m), m.sidebar.StatusFilter())
	}
}

func TestSelectRetriesThenGivesUpSilently(t *testing.T) {
	m := statusModel(t, statusSessions())
	before := selectedID(m)
	m.requestSelect("%99")
	for i := 0; i < selectAttempts-1; i++ {
		m.applyPendingSelect()
		if m.pendingSelect == "" {
			t.Fatalf("gave up after %d loads, want %d", i+1, selectAttempts)
		}
	}
	m.applyPendingSelect()
	if m.pendingSelect != "" || selectedID(m) != before || m.err != nil {
		t.Errorf("pending %q selected %q (was %q) err %v", m.pendingSelect, selectedID(m), before, m.err)
	}
}

func TestSelectFindsSessionThatAppearsLater(t *testing.T) {
	all := statusSessions()
	m := statusModel(t, all[:3])
	m.requestSelect("%5")
	m.applyPendingSelect()
	if m.pendingSelect == "" {
		t.Fatal("request dropped before the session was listed")
	}
	m.sessions = all
	m.sidebar.SetSessions(all)
	m.applyPendingSelect()
	if selectedID(m) != "b-wait" {
		t.Errorf("selected %q, want b-wait", selectedID(m))
	}
}

func TestSelectTakesOverPassthroughAndFocusesTarget(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SelectByID("a-idle")
	m.afterSelectionChange()
	m.setFocus(ModePassthrough)
	m.requestSelect("%5")
	m.applyPendingSelect()
	if got := selectedID(m); got != "b-wait" || m.mode != ModePassthrough || m.pendingSelect != "" {
		t.Errorf("selected %q, mode %v, pending %q; want b-wait focused", got, m.mode, m.pendingSelect)
	}
}

func TestSelectWaitsWhileRenamingAndKeyCancels(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.mode = ModeRenameSession
	m.requestSelect("%5")
	m.applyPendingSelect()
	if m.pendingSelect == "" || m.mode != ModeRenameSession {
		t.Error("a request must not clobber a text input; it waits")
	}
	m.mode = ModeList
	typeKeys(t, m, "j")
	if m.pendingSelect != "" {
		t.Error("a keystroke means the user took over")
	}
}

func TestHandoffRequestSelectsSessionAndIsPolledFromOwnPane(t *testing.T) {
	m := statusModel(t, statusSessions())
	h := &fakeHandoff{pending: map[string]string{"%30": "%6"}}
	m.handoff, m.selfPane = h, "%30"
	cmd := m.pollSelectCmd()
	if cmd == nil {
		t.Fatal("no poll command")
	}
	msg := cmd()
	req, ok := msg.(selectRequestMsg)
	if !ok || req.target != "%6" {
		t.Fatalf("poll returned %#v", msg)
	}
	next, _ := m.Update(req)
	*m = next.(Model)
	if got := selectedID(m); got != "b-done" {
		t.Errorf("selected %q, want b-done", got)
	}
	if got := m.pollSelectCmd()(); got != nil {
		t.Errorf("request must be consumed, polled %#v", got)
	}
	m.selfPane = ""
	if m.pollSelectCmd() != nil {
		t.Error("no pane, no polling")
	}
}

func listedHas(m *Model, id string) bool {
	for _, it := range m.sidebar.FlatItems() {
		if !it.IsGroup && it.Session.ID == id {
			return true
		}
	}
	return false
}
