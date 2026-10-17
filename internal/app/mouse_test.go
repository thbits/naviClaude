package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
	"github.com/thbits/naviClaude/internal/ui"
)

// Terminal rows of clickModel's list (titleRows of title bar, then the
// SESSIONS row): group header, "one" (selected, 2 lines), "two", spacer,
// Closed. clickModel checks titleRows against the rendered title bar.
const (
	titleRows = 1
	yHeader   = titleRows + 1
	yOne      = yHeader + 1
	yTwo      = yOne + 2
	ySpacer   = yTwo + 2
	yClosed   = ySpacer + 1
	yBlank    = 20
	xList     = 5
)

// clickModel is a 100x30 layout (sidebar 30 wide) with two idle sessions in
// one tmux group and a collapsed Closed group. Its targets all live in
// naviClaude's own window, so selection never resizes a real tmux window.
func clickModel(t *testing.T) (*Model, *time.Time) {
	t.Helper()
	now := time.Now()
	sessions := []*session.Session{
		{ID: "a1", DisplayName: "one", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.1", Status: session.StatusIdle, LastActivity: now},
		{ID: "a2", DisplayName: "two", TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0.2", Status: session.StatusIdle, LastActivity: now},
		{ID: "c1", DisplayName: "old", Status: session.StatusClosed, LastActivity: now.Add(-time.Hour)},
	}
	m := newFocusModel(0, sessions)
	m.width, m.height = 100, 30
	m.sidebarWidthPct = 30
	m.currentTmuxWin = "zzmouse:0"
	m.keys = DefaultKeyMap()
	m.search = ui.NewSearch()
	m.allSessions = sessions
	var hints []ui.StatusHintInput
	for _, h := range m.keys.StatusHints() {
		hints = append(hints, ui.StatusHintInput{Key: h.Key, Desc: h.Desc})
	}
	m.statusbar.SetKeyHints(hints)
	m.resizeComponents()
	m.sidebar.SetBreathingFrame(0) // re-render rows at the new width, as the breathing tick does
	m.mode = ModeList

	if h := lipgloss.Height(m.renderTitleBar()); h != titleRows {
		t.Fatalf("precondition: title bar is %d rows, row constants assume %d", h, titleRows)
	}
	clk := now
	m.now = func() time.Time { return clk }
	if got := m.sidebarHit(xList, yOne); got.Kind != ui.HitSession || got.Session.ID != "a1" {
		t.Fatalf("precondition: Y=%d should hit session a1, got %+v", yOne, got)
	}
	return &m, &clk
}

func press(m *Model, btn tea.MouseButton, x, y int) tea.Cmd {
	next, cmd := m.handleMouse(tea.MouseMsg{Button: btn, Action: tea.MouseActionPress, X: x, Y: y})
	*m = next.(Model)
	return cmd
}

func release(m *Model, x, y int) {
	next, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	*m = next.(Model)
}

func selectedID(m *Model) string {
	if s := m.sidebar.SelectedSession(); s != nil {
		return s.ID
	}
	return ""
}

func TestMouseClickSelectsRow(t *testing.T) {
	tests := []struct {
		name     string
		from     Mode
		x, y     int
		wantSel  string
		wantMode Mode
	}{
		{"row in list mode", ModeList, xList, yTwo, "a2", ModeList},
		{"second line of row", ModeList, xList, yTwo + 1, "a2", ModeList},
		{"row from passthrough focuses list", ModePassthrough, xList, yTwo, "a2", ModeList},
		{"spacer keeps selection", ModeList, xList, ySpacer, "a1", ModeList},
		{"blank space keeps selection", ModeChangedFiles, xList, yBlank, "a1", ModeList},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := clickModel(t)
			m.mode = tt.from
			press(m, tea.MouseButtonLeft, tt.x, tt.y)
			if got := selectedID(m); got != tt.wantSel {
				t.Errorf("selected = %q, want %q", got, tt.wantSel)
			}
			if m.mode != tt.wantMode {
				t.Errorf("mode = %v, want %v", m.mode, tt.wantMode)
			}
		})
	}
}

func TestMouseDoubleClick(t *testing.T) {
	tests := []struct {
		name     string
		gap      time.Duration
		secondY  int
		wantMode Mode
	}{
		{"fast second press focuses session", 150 * time.Millisecond, yTwo, ModePassthrough},
		{"slow second press only selects", 600 * time.Millisecond, yTwo, ModeList},
		{"fast press on another row only selects", 150 * time.Millisecond, yOne, ModeList},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, clk := clickModel(t)
			press(m, tea.MouseButtonLeft, xList, yTwo)
			release(m, xList, yTwo)
			*clk = clk.Add(tt.gap)
			// The selected row may have moved; hit wherever the row now is.
			y := tt.secondY
			if tt.secondY == yTwo {
				y = rowOf(t, m, "a2")
			}
			press(m, tea.MouseButtonLeft, xList, y)
			if m.mode != tt.wantMode {
				t.Errorf("mode = %v, want %v", m.mode, tt.wantMode)
			}
		})
	}
}

// rowOf returns the first terminal row that hits session id.
func rowOf(t *testing.T, m *Model, id string) int {
	t.Helper()
	for y := 0; y < m.height; y++ {
		if h := m.sidebarHit(xList, y); h.Kind == ui.HitSession && h.Session.ID == id {
			return y
		}
	}
	t.Fatalf("session %s not on screen", id)
	return -1
}

func TestMouseClickGroupHeaderToggles(t *testing.T) {
	m, _ := clickModel(t)
	before := len(m.sidebar.FlatItems())
	press(m, tea.MouseButtonLeft, xList, yClosed)
	if got := len(m.sidebar.FlatItems()); got != before+1 {
		t.Fatalf("flat items = %d, want %d (Closed expanded)", got, before+1)
	}
	if m.sidebar.SelectedGroupName() != ui.ClosedGroupName {
		t.Errorf("cursor = %q, want the Closed header", m.sidebar.SelectedGroupName())
	}
	press(m, tea.MouseButtonLeft, xList, yClosed)
	if got := len(m.sidebar.FlatItems()); got != before {
		t.Errorf("flat items = %d, want %d (Closed collapsed again)", got, before)
	}
}

func TestMouseRightClickOpensMenuForRow(t *testing.T) {
	m, _ := clickModel(t)
	press(m, tea.MouseButtonRight, xList, yTwo)
	if m.mode != ModeContextMenu || !m.contextMenu.IsVisible() {
		t.Fatalf("mode = %v, menu visible = %v; want context menu", m.mode, m.contextMenu.IsVisible())
	}
	if got := m.contextMenu.Session(); got == nil || got.ID != "a2" {
		t.Errorf("menu session = %+v, want a2 (the clicked row)", got)
	}
	if selectedID(m) != "a2" {
		t.Errorf("selected = %q, want a2", selectedID(m))
	}
	if x, y, _, _ := m.contextMenu.Rect(); x != xList || y != yTwo {
		t.Errorf("menu at (%d,%d), want the click position (%d,%d)", x, y, xList, yTwo)
	}

	// Right-clicking another row while open re-opens the menu there.
	press(m, tea.MouseButtonRight, xList, rowOf(t, m, "a1"))
	if got := m.contextMenu.Session(); !m.contextMenu.IsVisible() || got == nil || got.ID != "a1" {
		t.Errorf("menu session = %+v (visible %v), want a1", got, m.contextMenu.IsVisible())
	}
}

func TestMouseRightClickEmptySpaceDoesNothing(t *testing.T) {
	for _, y := range []int{yBlank, ySpacer, yHeader} {
		m, _ := clickModel(t)
		press(m, tea.MouseButtonRight, xList, y)
		if m.contextMenu.IsVisible() || m.mode != ModeList {
			t.Errorf("Y=%d: menu opened (mode %v)", y, m.mode)
		}
	}
}

func TestMouseContextMenuItemClick(t *testing.T) {
	m, _ := clickModel(t)
	press(m, tea.MouseButtonRight, xList, yTwo)
	x, y, _, _ := m.contextMenu.Rect()
	// Active-session items: 0 Focus, 1 Jump, 2 separator, 3 Detail.
	press(m, tea.MouseButtonLeft, x+1, y+1+2) // separator: ignored
	if !m.contextMenu.IsVisible() {
		t.Fatal("clicking a separator closed the menu")
	}
	cmd := press(m, tea.MouseButtonLeft, x+2, y+1+3)
	if m.contextMenu.IsVisible() {
		t.Error("menu still visible after running an item")
	}
	if m.mode != ModeDetail || !m.detail.IsVisible() {
		t.Errorf("mode = %v, detail visible = %v; want the Detail action to run", m.mode, m.detail.IsVisible())
	}
	if cmd == nil {
		t.Error("detail action should return its data-load command")
	}
}

func TestMouseClickOutsideClosesOverlays(t *testing.T) {
	t.Run("context menu", func(t *testing.T) {
		m, _ := clickModel(t)
		press(m, tea.MouseButtonRight, xList, yTwo)
		press(m, tea.MouseButtonLeft, 80, 20)
		if m.contextMenu.IsVisible() || m.mode != ModeList {
			t.Errorf("menu visible = %v, mode = %v; want closed", m.contextMenu.IsVisible(), m.mode)
		}
		if selectedID(m) != "a2" {
			t.Errorf("click outside acted underneath: selected = %q", selectedID(m))
		}
	})

	t.Run("help", func(t *testing.T) {
		m, _ := clickModel(t)
		m.help.Toggle()
		m.mode = ModeHelp
		x, y, w, h := ui.OverlayRect(m.width, m.height, m.help.View())
		press(m, tea.MouseButtonLeft, x+w/2, y+h/2)
		if !m.help.IsVisible() {
			t.Fatal("click inside help closed it")
		}
		press(m, tea.MouseButtonLeft, xList, yTwo)
		if m.help.IsVisible() || m.mode != ModeList {
			t.Errorf("help visible = %v, mode = %v; want closed", m.help.IsVisible(), m.mode)
		}
		if selectedID(m) != "a1" {
			t.Errorf("click under the help overlay selected %q", selectedID(m))
		}
	})
}

func TestMouseWheelMovesSelection(t *testing.T) {
	m, _ := clickModel(t)
	m.mode = ModePassthrough
	press(m, tea.MouseButtonWheelDown, xList, yBlank)
	if selectedID(m) != "a2" || m.mode != ModeList {
		t.Fatalf("after wheel down: selected = %q mode = %v, want a2 in list mode", selectedID(m), m.mode)
	}
	press(m, tea.MouseButtonWheelDown, xList, yBlank) // onto the Closed header
	if m.sidebar.SelectedGroupName() != ui.ClosedGroupName {
		t.Errorf("after 2nd wheel down: group = %q, want Closed", m.sidebar.SelectedGroupName())
	}
	press(m, tea.MouseButtonWheelUp, xList, yBlank)
	if selectedID(m) != "a2" {
		t.Errorf("after wheel up: selected = %q, want a2", selectedID(m))
	}
}

func TestMouseLongPressOpensMenu(t *testing.T) {
	tests := []struct {
		name     string
		held     time.Duration
		releaseY int
		between  bool // another button event between press and release
		killMid  bool // the kill prompt opened by keyboard mid-hold
		wantMenu bool
	}{
		{"held past threshold", longPressThreshold, yTwo, false, false, true},
		{"short click", longPressThreshold - time.Millisecond, yTwo, false, false, false},
		{"released on another row", 2 * longPressThreshold, yOne, false, false, false},
		{"wheel in between cancels", 2 * longPressThreshold, yTwo, true, false, false},
		{"kill prompt opened mid-hold", 2 * longPressThreshold, yTwo, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, clk := clickModel(t)
			press(m, tea.MouseButtonLeft, xList, yTwo)
			if tt.between {
				press(m, tea.MouseButtonWheelUp, 50, 10) // over the preview
			}
			if tt.killMid {
				m.confirmKill = true
			}
			*clk = clk.Add(tt.held)
			release(m, xList, tt.releaseY)
			if got := m.contextMenu.IsVisible(); got != tt.wantMenu {
				t.Fatalf("menu visible = %v, want %v", got, tt.wantMenu)
			}
			if !tt.wantMenu {
				return
			}
			if s := m.contextMenu.Session(); s == nil || s.ID != "a2" || m.mode != ModeContextMenu {
				t.Errorf("menu session = %+v mode = %v, want a2 in context-menu mode", s, m.mode)
			}
			if x, y, _, _ := m.contextMenu.Rect(); x != xList || y != yTwo {
				t.Errorf("menu at (%d,%d), want the press position (%d,%d)", x, y, xList, yTwo)
			}
		})
	}
}

// A long press must not arm a double-click: the next quick press on the same
// row only selects it.
func TestMouseLongPressDoesNotCountTowardDoubleClick(t *testing.T) {
	m, clk := clickModel(t)
	press(m, tea.MouseButtonLeft, xList, yTwo)
	*clk = clk.Add(longPressThreshold)
	release(m, xList, yTwo)
	if !m.contextMenu.IsVisible() {
		t.Fatal("precondition: long press should open the menu")
	}
	m.closeContextMenu()
	*clk = clk.Add(50 * time.Millisecond)
	// Pretend the double-click window were long enough to span the hold.
	m.lastClickAt = *clk
	press(m, tea.MouseButtonLeft, xList, rowOf(t, m, "a2"))
	if m.mode != ModeList {
		t.Errorf("mode = %v, want list (no double-click after a long press)", m.mode)
	}
}

// hintX finds the column of the status-bar hint labelled key.
func hintX(t *testing.T, m *Model, key string) int {
	t.Helper()
	for x := 0; x < m.width; x++ {
		if k, ok := m.statusbar.HintAt(x); ok && k == key {
			return x
		}
	}
	t.Fatalf("hint %q not on the status bar", key)
	return -1
}

func TestMouseStatusBarHintClick(t *testing.T) {
	t.Run("list hint runs its key", func(t *testing.T) {
		m, _ := clickModel(t)
		press(m, tea.MouseButtonLeft, hintX(t, m, "/")+3, m.height-1) // on the description
		if !m.search.IsActive() || m.mode != ModeSearch {
			t.Errorf("search active = %v mode = %v, want search opened", m.search.IsActive(), m.mode)
		}
	})

	t.Run("passthrough hint runs its key", func(t *testing.T) {
		m, _ := clickModel(t)
		m.setFocus(ModePassthrough)
		press(m, tea.MouseButtonLeft, hintX(t, m, "Ctrl+]"), m.height-1)
		if m.mode != ModeList {
			t.Errorf("mode = %v, want list after clicking the Ctrl+] hint", m.mode)
		}
	})

	t.Run("context menu Esc hint cancels", func(t *testing.T) {
		m, _ := clickModel(t)
		press(m, tea.MouseButtonRight, xList, yTwo)
		press(m, tea.MouseButtonLeft, hintX(t, m, "Esc"), m.height-1)
		if m.contextMenu.IsVisible() || m.mode != ModeList {
			t.Errorf("menu visible = %v mode = %v, want cancelled", m.contextMenu.IsVisible(), m.mode)
		}
	})

	t.Run("separator between hints does nothing", func(t *testing.T) {
		m, _ := clickModel(t)
		x := hintX(t, m, "/")
		for ; x < m.width; x++ {
			if _, ok := m.statusbar.HintAt(x); !ok {
				break
			}
		}
		press(m, tea.MouseButtonLeft, x, m.height-1)
		if m.mode != ModeList || m.search.IsActive() {
			t.Errorf("mode = %v, want list unchanged", m.mode)
		}
	})
}

func TestHintKeyMsg(t *testing.T) {
	tests := []struct {
		label string
		want  string
		ok    bool
	}{
		{"Enter", "enter", true},
		{"Esc", "esc", true},
		{"Tab", "tab", true},
		{"Ctrl+]", "ctrl+]", true},
		{"Ctrl+f", "ctrl+f", true},
		{"K", "K", true},
		{"k", "k", true},
		{"/", "/", true},
		{"any key", "esc", true},
		{"j/k", "", false},
	}
	for _, tt := range tests {
		got, ok := hintKeyMsg(tt.label)
		if ok != tt.ok || (ok && got.String() != tt.want) {
			t.Errorf("hintKeyMsg(%q) = %q,%v want %q,%v", tt.label, got.String(), ok, tt.want, tt.ok)
		}
	}
}

func TestMouseResumePicker(t *testing.T) {
	open := func(t *testing.T) *Model {
		m, _ := clickModel(t)
		m.resumePicker = ui.NewSessionPicker()
		m.resumePicker.SetSize(m.width, m.height)
		m.resumePicker.Show([]string{"alpha", "beta", "gamma"}, "")
		m.pendingResumeSession = &session.Session{ID: "c1", Status: session.StatusClosed}
		m.mode = ModeResumePicker
		return m
	}
	rowY := func(m *Model, row int) (int, int) {
		x, y, _, _ := ui.OverlayRect(m.width, m.height, m.resumePicker.View())
		return x + 4, y + 5 + row // border + padding + title, input, separator
	}

	t.Run("click selects then confirms", func(t *testing.T) {
		m := open(t)
		x, y := rowY(m, 1)
		press(m, tea.MouseButtonLeft, x, y)
		if name, _ := m.resumePicker.Selected(); name != "beta" || !m.resumePicker.IsVisible() {
			t.Fatalf("selected = %q visible = %v, want beta still open", name, m.resumePicker.IsVisible())
		}
		cmd := press(m, tea.MouseButtonLeft, x, y)
		if m.resumePicker.IsVisible() || m.mode != ModeList || cmd == nil {
			t.Errorf("visible = %v mode = %v cmd nil = %v; want confirmed resume", m.resumePicker.IsVisible(), m.mode, cmd == nil)
		}
	})

	t.Run("wheel moves the cursor", func(t *testing.T) {
		m := open(t)
		press(m, tea.MouseButtonWheelDown, 50, 15)
		if name, _ := m.resumePicker.Selected(); name != "beta" {
			t.Errorf("selected = %q after wheel down, want beta", name)
		}
	})

	t.Run("click outside cancels", func(t *testing.T) {
		m := open(t)
		press(m, tea.MouseButtonLeft, 0, 0)
		if m.resumePicker.IsVisible() || m.pendingResumeSession != nil || m.mode != ModeList {
			t.Errorf("visible = %v pending = %v mode = %v; want cancelled", m.resumePicker.IsVisible(), m.pendingResumeSession, m.mode)
		}
	})
}

func TestMouseStatsFilterClick(t *testing.T) {
	m, _ := clickModel(t)
	m.statsModel.SetSize(m.width, m.height)
	m.statsModel.Show()
	m.statsModel.SetStats(&stats.Stats{})
	m.mode = ModeStats

	view := m.statsModel.View()
	px, py, _, _ := ui.OverlayRect(m.width, m.height, view)
	var lx, ly int
	for i, line := range strings.Split(view, "\n") {
		plain := ansi.Strip(line)
		if j := strings.Index(plain, "This week"); j >= 0 {
			lx, ly = px+ansi.StringWidth(plain[:j])+2, py+i
			break
		}
	}
	if ly == 0 {
		t.Fatal("filter labels not found in the stats view")
	}
	cmd := press(m, tea.MouseButtonLeft, lx, ly)
	if m.statsModel.SetFilter(ui.FilterWeek) || cmd == nil {
		t.Errorf("filter not week or cmd nil (%v), want week + recompute", cmd == nil)
	}

	press(m, tea.MouseButtonLeft, 0, 0)
	if m.statsModel.IsVisible() || m.mode != ModeList {
		t.Errorf("stats visible = %v mode = %v, want closed by click outside", m.statsModel.IsVisible(), m.mode)
	}
}

// viewRow finds the terminal row whose sidebar column renders text, reading the
// real composed View so the test checks the offsets end to end.
func viewRow(t *testing.T, m *Model, text string) int {
	t.Helper()
	for y, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Truncate(ansi.Strip(line), m.sidebarWidth(), ""), text) {
			return y
		}
	}
	t.Fatalf("%q not rendered in the sidebar", text)
	return -1
}

// Clicking where a row is drawn selects it, whatever sits above the list.
func TestMouseClickMatchesRenderedRows(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *Model)
		mode  Mode
	}{
		{"list", func(m *Model) {}, ModeList},
		{"passthrough (unfocused pane title)", func(m *Model) { m.setFocus(ModePassthrough) }, ModeList},
		{"search (input stacked above the list)", func(m *Model) {
			next, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			*m = next.(Model)
		}, ModeSearch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := clickModel(t)
			tt.setup(m)
			press(m, tea.MouseButtonLeft, xList, viewRow(t, m, "two"))
			if selectedID(m) != "a2" {
				t.Errorf("selected = %q, want a2", selectedID(m))
			}
			if m.mode != tt.mode {
				t.Errorf("mode = %v, want %v", m.mode, tt.mode)
			}
		})
	}
}
