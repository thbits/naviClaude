package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
	"github.com/thbits/naviClaude/internal/ui"
)

// filesHarness reports a fixed changed-files list and a settable change stamp.
type filesHarness struct {
	coreOnly
	stamp *time.Time
}

func (h filesHarness) LoadChangedFiles(*session.Session) ([]session.ChangedFile, error) {
	return []session.ChangedFile{{Path: "/w/a.go", Added: 3, Removed: 1}}, nil
}
func (h filesHarness) ChangeStamp(*session.Session) time.Time { return *h.stamp }

var _ harness.ChangedFilesProvider = filesHarness{}

func send(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	next, cmd := m.Update(msg)
	*m = next.(Model)
	return cmd
}

func TestStatsResultForStaleFilterIsDropped(t *testing.T) {
	m, _ := clickModel(t)
	m.statsModel.Show()
	m.mode = ModeStats
	m.statsModel.CycleFilter() // All -> Week while All is still computing

	send(t, m, statsComputeMsg{stats: &stats.Stats{Filter: "all"}})
	if !m.statsModel.IsLoading() {
		t.Fatal("the All result must not land under the Week filter")
	}
	send(t, m, statsComputeMsg{stats: &stats.Stats{Filter: "week"}})
	if m.statsModel.IsLoading() {
		t.Error("the Week result must apply")
	}
}

func TestStatsLateErrorAfterCloseKeepsMode(t *testing.T) {
	m, _ := clickModel(t)
	m.mode = ModePassthrough
	send(t, m, statsComputeMsg{err: errTest})
	if m.mode != ModePassthrough || m.err != nil {
		t.Errorf("mode = %v err = %v, want a late error for a closed popup ignored", m.mode, m.err)
	}
}

var errTest = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "boom" }

// metricsFor applies loaded metrics for the selected session the way a
// finished load does.
func metricsFor(t *testing.T, m *Model, id string) {
	t.Helper()
	m.selectPreviewSession(m.sidebar.SelectedSession())
	m.metricsSessionID = id
	send(t, m, metricsMsg{sessionID: id, metrics: &session.SessionMetrics{
		StartTime: time.Now().Add(-time.Hour), TokensUsed: 150_000, ContextLimit: 200_000, ModelID: "claude-opus-5-5",
	}})
}

func TestPlaceholderDoesNotShowPreviousMetrics(t *testing.T) {
	m, _ := clickModel(t)
	metricsFor(t, m, "a1")
	if !strings.Contains(ansi.Strip(m.preview.View()), "Opus 5.5") {
		t.Fatal("precondition: a1's model shows in the preview header")
	}

	placeholder := &session.Session{TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.9", Status: session.StatusActive, LastActivity: time.Now()}
	m.sessions = append([]*session.Session{placeholder}, m.sessions...)
	m.sidebar.SetSessions(m.sessions)
	m.sidebar.SelectByTarget(placeholder.TmuxTarget)
	m.afterSelectionChange()

	if m.metricsSessionID != "" {
		t.Errorf("metricsSessionID = %q, want cleared for a placeholder", m.metricsSessionID)
	}
	if v := ansi.Strip(m.preview.View()); strings.Contains(v, "Opus 5.5") || strings.Contains(v, "⏱") {
		t.Errorf("placeholder header shows a1's metrics:\n%s", v)
	}
	if strings.Contains(ansi.Strip(m.sidebar.View()), "ctx ") {
		t.Error("placeholder row shows a1's context bar")
	}
}

func TestRefreshFallbackSelectionReloadsMetrics(t *testing.T) {
	m, _ := clickModel(t)
	m.aliasStore = session.NewAliasStore(filepath.Join(t.TempDir(), "aliases.json"))
	metricsFor(t, m, "a1")

	// a1 disappears; the cursor falls back to a neighbor.
	two := &session.Session{ID: "a2", DisplayName: "two", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.2", Status: session.StatusIdle, LastActivity: time.Now()}
	cmd := m.applySessionRefresh(sessionRefreshMsg{active: []*session.Session{two}})
	if selectedID(m) != "a2" {
		t.Fatalf("precondition: selection fell back to %q, want a2", selectedID(m))
	}
	if m.metricsSessionID != "a2" || cmd == nil {
		t.Errorf("metricsSessionID = %q cmd nil = %v, want a reload for a2", m.metricsSessionID, cmd == nil)
	}
	if strings.Contains(ansi.Strip(m.preview.View()), "Opus 5.5") {
		t.Error("a2's header shows a1's model")
	}
}

func TestRepoCountsLoadWithChangedFilesPanelClosed(t *testing.T) {
	m, _ := clickModel(t)
	stamp := time.Unix(1000, 0)
	m.registry = harness.NewRegistry(filesHarness{stamp: &stamp})
	m.rightPanelOpen = false

	if cmd := m.afterSelectionChange(); cmd == nil {
		t.Fatal("selection change must load something")
	}
	if m.changedFilesSessionID != "a1" {
		t.Fatalf("changedFilesSessionID = %q, want a1 loaded for the repo line", m.changedFilesSessionID)
	}
	msg := loadChangedFilesCmd(m.harnessFor(m.sidebar.SelectedSession()), m.sidebar.SelectedSession())()
	send(t, m, msg)

	if cmd := m.maybeRefreshChangedFilesCmd(); cmd != nil {
		t.Error("an unchanged stamp must not re-diff")
	}
	stamp = stamp.Add(time.Second)
	if cmd := m.maybeRefreshChangedFilesCmd(); cmd == nil {
		t.Error("an advanced stamp must refresh the repo-line counts with the panel closed")
	}
}

// A trackpad flick sends dozens of wheel ticks; only the final selection may
// pay for the pane resize and detail loads, once input settles.
func TestWheelFlickRunsOneDeferredSync(t *testing.T) {
	m, _ := clickModel(t)
	m.afterSelectionChange()
	startTarget, startMetrics := m.previewTarget, m.metricsSessionID

	const n = 5
	for i := 0; i < n; i++ {
		down := i%2 == 0 // a1 -> a2 -> a1 ... ends on a2
		btn := tea.MouseButtonWheelUp
		if down {
			btn = tea.MouseButtonWheelDown
		}
		if cmd := press(m, btn, xList, yBlank); cmd == nil {
			t.Fatalf("wheel %d: want a settle tick", i)
		}
		if m.previewTarget != startTarget || m.metricsSessionID != startMetrics {
			t.Fatalf("wheel %d ran the heavy sync: target %q metrics %q", i, m.previewTarget, m.metricsSessionID)
		}
	}
	if selectedID(m) != "a2" {
		t.Fatalf("cursor = %q, want a2 moved immediately", selectedID(m))
	}

	final := m.selectionGen
	for gen := final - n + 1; gen < final; gen++ {
		if cmd := send(t, m, selectionSettledMsg{gen: gen}); cmd != nil || m.metricsSessionID != startMetrics {
			t.Fatalf("stale settle %d synced: metrics %q", gen, m.metricsSessionID)
		}
	}
	if cmd := send(t, m, selectionSettledMsg{gen: final}); cmd == nil {
		t.Fatal("final settle must load the selection's details")
	}
	if m.previewTarget != "zzmouse:0.2" || m.metricsSessionID != "a2" {
		t.Errorf("after settle: target %q metrics %q, want a2", m.previewTarget, m.metricsSessionID)
	}
	if cmd := send(t, m, selectionSettledMsg{gen: final}); cmd != nil {
		t.Error("a repeated settle must be a no-op")
	}
}

// j/k repeats share the debounce; a click right after still syncs at once.
func TestKeyRepeatDebouncesAndClickSyncsImmediately(t *testing.T) {
	m, _ := clickModel(t)
	m.afterSelectionChange()
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	*m = next.(Model)
	if selectedID(m) != "a2" || m.metricsSessionID != "a1" || !m.selectionPending {
		t.Fatalf("after j: selected %q metrics %q pending %v, want a2 deferred", selectedID(m), m.metricsSessionID, m.selectionPending)
	}
	if m.capturePreviewCmd() != nil {
		t.Error("no capture while the preview still shows the previous session")
	}
	pending := m.selectionGen
	press(m, tea.MouseButtonLeft, xList, yOne)
	if m.metricsSessionID != "a1" || m.previewTarget != "zzmouse:0.1" || m.selectionPending {
		t.Fatalf("click: metrics %q target %q pending %v, want a1 synced at once", m.metricsSessionID, m.previewTarget, m.selectionPending)
	}
	if cmd := send(t, m, selectionSettledMsg{gen: pending}); cmd != nil {
		t.Error("a settle superseded by a click must be dropped")
	}
}

// statusLine is the status bar's hint row as plain text.
func statusLine(m *Model) string {
	lines := strings.Split(ansi.Strip(m.statusbar.View()), "\n")
	return lines[len(lines)-1]
}

func statusText(m *Model) string { return strings.Join(strings.Fields(statusLine(m)), " ") }

func TestHelpAndKillPromptSwapStatusHints(t *testing.T) {
	m, _ := clickModel(t)
	m.help = ui.NewHelp()
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keys.Help)})
	*m = next.(Model)
	if !strings.Contains(statusText(m), "any key close") {
		t.Errorf("help open: status = %q, want the close hint", statusText(m))
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	*m = next.(Model)

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keys.KillSession)})
	*m = next.(Model)
	if !m.confirmKill || !strings.Contains(statusText(m), "y kill") {
		t.Fatalf("kill prompt: confirm %v status = %q, want the y/cancel hints", m.confirmKill, statusText(m))
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	*m = next.(Model)
	if m.confirmKill || strings.Contains(statusText(m), "y kill") {
		t.Errorf("after cancel: status = %q, want list hints back", statusText(m))
	}
}

// Clicking an inert hint (j/k) under an open menu is not a click outside it.
func TestInertHintClickKeepsContextMenuOpen(t *testing.T) {
	m, _ := clickModel(t)
	press(m, tea.MouseButtonRight, xList, yOne)
	if !m.contextMenu.IsVisible() {
		t.Fatal("precondition: context menu open")
	}
	x := ansi.StringWidth(strings.SplitN(statusLine(m), "j/k", 2)[0])
	if !strings.Contains(statusLine(m), "j/k") {
		t.Fatalf("no j/k hint in %q", statusLine(m))
	}
	press(m, tea.MouseButtonLeft, x, m.height-1)
	if !m.contextMenu.IsVisible() {
		t.Error("clicking the inert j/k hint closed the menu")
	}
}

// The row selected at startup never went through a selection sync; the tick
// must still load its repo-line counts.
func TestRepoCountsLoadForStartupSelection(t *testing.T) {
	m, _ := clickModel(t)
	stamp := time.Unix(1000, 0)
	m.registry = harness.NewRegistry(filesHarness{stamp: &stamp})
	if cmd := m.maybeRefreshChangedFilesCmd(); cmd == nil || m.changedFilesSessionID != "a1" {
		t.Errorf("tick: cmd nil = %v changedFilesSessionID = %q, want a1 loaded", cmd == nil, m.changedFilesSessionID)
	}
}

func TestSessionTickSkipsWhileRefreshInFlight(t *testing.T) {
	m, _ := clickModel(t)
	send(t, m, tickSessionMsg{})
	if !m.refreshInFlight {
		t.Fatal("tick must mark the refresh in flight")
	}
	inFlight := len(send(t, m, tickSessionMsg{})().(tea.BatchMsg))
	send(t, m, sessionRefreshMsg{err: errTestRefresh})
	if m.refreshInFlight {
		t.Error("a landed refresh must clear the in-flight gate")
	}
	if landed := len(send(t, m, tickSessionMsg{})().(tea.BatchMsg)); landed != inFlight+1 {
		t.Errorf("tick cmds: %d in flight, %d after landing; only the latter may refresh", inFlight, landed)
	}
}

var errTestRefresh = errors.New("refresh failed")

// A just-started Claude has no per-PID metadata yet, so detection briefly
// borrows the cwd's newest transcript ID (another live session's) before the
// real one appears. The cursor must stay on the new pane through that.
func TestNewSessionKeepsFocusWhenDetectedIDChanges(t *testing.T) {
	m, _ := clickModel(t)
	m.aliasStore = session.NewAliasStore(filepath.Join(t.TempDir(), "aliases.json"))
	m.sidebar.SelectByID("a2")

	send(t, m, newSessionMsg{tmuxTarget: "zzmouse:0.9", tmuxSession: "zzmouse", cwd: "/w", harness: "claude"})
	if s := m.sidebar.SelectedSession(); s == nil || s.TmuxTarget != "zzmouse:0.9" {
		t.Fatalf("precondition: placeholder not selected, got %+v", s)
	}

	now := time.Now()
	one := &session.Session{ID: "a1", DisplayName: "one", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.1", TmuxPaneID: "%1", Status: session.StatusIdle, LastActivity: now}
	two := &session.Session{ID: "a2", DisplayName: "two", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.2", TmuxPaneID: "%2", Status: session.StatusIdle, LastActivity: now}
	borrowed := &session.Session{ID: "a1", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.9", TmuxPaneID: "%9", Status: session.StatusActive, LastActivity: now}
	m.applySessionRefresh(sessionRefreshMsg{active: []*session.Session{one, two, borrowed}})
	if s := m.sidebar.SelectedSession(); s == nil || s.TmuxTarget != "zzmouse:0.9" {
		t.Fatalf("after first detection cursor on %+v, want zzmouse:0.9", s)
	}

	real := &session.Session{ID: "n1", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.9", TmuxPaneID: "%9", Status: session.StatusActive, LastActivity: now}
	m.applySessionRefresh(sessionRefreshMsg{active: []*session.Session{one, two, real}})
	if s := m.sidebar.SelectedSession(); s == nil || s.TmuxTarget != "zzmouse:0.9" {
		t.Errorf("after ID resolved cursor jumped to %+v, want zzmouse:0.9", s)
	}
}
