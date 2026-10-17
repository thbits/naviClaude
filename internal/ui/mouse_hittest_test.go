package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

// hitSidebar builds a 40-wide sidebar with groups alpha {one, two}, beta
// {three}, and a collapsed Closed group. The cursor starts on "one", which
// renders 3 lines (name, summary, activity).
func hitSidebar(height int) SidebarModel {
	now := time.Now()
	sb := NewSidebar(40, 20)
	sb.SetSize(40, height)
	sb.SetSessions([]*session.Session{
		{ID: "1", DisplayName: "one", TmuxSession: "alpha", TmuxTarget: "alpha:0.0", Status: session.StatusIdle, LastActivity: now},
		{ID: "2", DisplayName: "two", TmuxSession: "alpha", TmuxTarget: "alpha:0.1", Status: session.StatusIdle, LastActivity: now},
		{ID: "3", DisplayName: "three", TmuxSession: "beta", TmuxTarget: "beta:0.0", Status: session.StatusIdle, LastActivity: now},
		{ID: "4", DisplayName: "gone", Status: session.StatusClosed, LastActivity: now},
	})
	return sb
}

type wantHit struct {
	kind  SidebarHitKind
	name  string // session DisplayName or group name
	index int
}

func checkHit(t *testing.T, got SidebarHit, want wantHit) {
	t.Helper()
	if got.Kind != want.kind {
		t.Fatalf("kind = %v, want %v (hit %+v)", got.Kind, want.kind, got)
	}
	switch got.Kind {
	case HitSession:
		if got.Session.DisplayName != want.name || got.Index != want.index {
			t.Errorf("session = %q@%d, want %q@%d", got.Session.DisplayName, got.Index, want.name, want.index)
		}
	case HitGroup:
		if got.Group != want.name || got.Index != want.index {
			t.Errorf("group = %q@%d, want %q@%d", got.Group, got.Index, want.name, want.index)
		}
	}
}

func TestSidebarHitTest(t *testing.T) {
	sb := hitSidebar(20)
	if sel := sb.SelectedSession(); sel == nil || sel.DisplayName != "one" {
		t.Fatalf("precondition: cursor should start on 'one', got %+v", sel)
	}

	// Content lines: 0 alpha | 1-3 one (selected) | 4-5 two | 6 spacer |
	// 7 beta | 8-9 three | 10 spacer | 11 Closed. Local y = line + 1.
	tests := []struct {
		name string
		x, y int
		want wantHit
	}{
		{"pane title", 5, 0, wantHit{kind: HitNone}},
		{"first group header", 5, 1, wantHit{HitGroup, "alpha", 0}},
		{"selected row line 1", 5, 2, wantHit{HitSession, "one", 1}},
		{"selected row summary", 5, 3, wantHit{HitSession, "one", 1}},
		{"selected row metrics line", 5, 4, wantHit{HitSession, "one", 1}},
		{"normal row line 1", 5, 5, wantHit{HitSession, "two", 2}},
		{"normal row line 2", 5, 6, wantHit{HitSession, "two", 2}},
		{"spacer before group", 5, 7, wantHit{kind: HitNone}},
		{"second group header", 5, 8, wantHit{HitGroup, "beta", 3}},
		{"row in second group", 5, 10, wantHit{HitSession, "three", 4}},
		{"spacer before Closed", 5, 11, wantHit{kind: HitNone}},
		{"collapsed Closed header", 5, 12, wantHit{HitGroup, ClosedGroupName, 5}},
		{"empty space below list", 5, 13, wantHit{kind: HitNone}},
		{"left of pane", -1, 2, wantHit{kind: HitNone}},
		{"right of pane", 40, 2, wantHit{kind: HitNone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkHit(t, sb.HitTest(tt.x, tt.y), tt.want)
		})
	}

	// Cross-check the line map against what View actually draws.
	lines := strings.Split(sb.View(), "\n")
	for y, text := range map[int]string{1: "alpha", 2: "one", 5: "two", 8: "beta", 9: "three", 12: ClosedGroupName} {
		if !strings.Contains(lines[y], text) {
			t.Errorf("rendered line %d = %q, want it to contain %q", y, lines[y], text)
		}
	}
}

// A short pane scrolls to keep the cursor visible; hits must follow YOffset.
func TestSidebarHitTestScrolled(t *testing.T) {
	sb := hitSidebar(5) // 1 title row + 4 list rows
	sb.MoveCursor(3)    // one -> two -> beta -> three
	if sel := sb.SelectedSession(); sel == nil || sel.DisplayName != "three" {
		t.Fatalf("precondition: cursor should be on 'three', got %+v", sel)
	}

	// Lines: 0 alpha | 1-2 one | 3-4 two | 5 spacer | 6 beta | 7-9 three
	// (selected). Showing 4 rows with three fully visible puts YOffset at 6.
	tests := []struct {
		y    int
		want wantHit
	}{
		{0, wantHit{kind: HitNone}},
		{1, wantHit{HitGroup, "beta", 3}},
		{2, wantHit{HitSession, "three", 4}},
		{4, wantHit{HitSession, "three", 4}},
		{5, wantHit{kind: HitNone}}, // below the viewport
	}
	for _, tt := range tests {
		checkHit(t, sb.HitTest(3, tt.y), tt.want)
	}
}

func TestSidebarToggleGroupAtCursor(t *testing.T) {
	sb := hitSidebar(20)
	sb.SetCursor(5) // Closed header
	sb.ToggleGroupAtCursor()
	if got := len(sb.FlatItems()); got != 7 {
		t.Fatalf("flat items after expanding Closed = %d, want 7", got)
	}
	if sb.SelectedGroupName() != ClosedGroupName {
		t.Errorf("cursor left the toggled header: %q", sb.SelectedGroupName())
	}
}

func TestContextMenuItemAt(t *testing.T) {
	m := NewContextMenu()
	m.SetTermSize(100, 30)
	m.Show(10, 5, &session.Session{Status: session.StatusActive}, MenuCaps{})
	// Items: 0 Focus, 1 Jump, 2 sep, 3 Detail, 4 sep, 5 Copy ID, 6 Copy path,
	// 7 sep, 8 Kill. The box starts at (10,5) with a 1-cell border.
	x, y, w, h := m.Rect()
	if x != 10 || y != 5 || h != 11 {
		t.Fatalf("rect = (%d,%d,%d,%d), want x=10 y=5 h=11", x, y, w, h)
	}

	tests := []struct {
		name   string
		px, py int
		want   int
		ok     bool
	}{
		{"first item", 12, 6, 0, true},
		{"jump", 12, 7, 1, true},
		{"separator", 12, 8, 0, false},
		{"detail", 12, 9, 3, true},
		{"kill (last)", 12, 14, 8, true},
		{"top border", 12, 5, 0, false},
		{"left border", 10, 6, 0, false},
		{"right border", 10 + w - 1, 6, 0, false},
		{"outside", 5, 6, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := m.ItemAt(tt.px, tt.py)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Errorf("ItemAt(%d,%d) = %d,%v want %d,%v", tt.px, tt.py, got, ok, tt.want, tt.ok)
			}
		})
	}

	// Near the bottom-right corner the menu clamps inside the terminal.
	m.Show(98, 28, &session.Session{Status: session.StatusActive}, MenuCaps{})
	x, y, w, h = m.Rect()
	if x+w != 100 || y+h != 30 {
		t.Errorf("clamped rect = (%d,%d,%d,%d), want right/bottom edge at 100/30", x, y, w, h)
	}
	if got, ok := m.ItemAt(x+2, y+1); !ok || got != 0 {
		t.Errorf("ItemAt first row of clamped menu = %d,%v want 0,true", got, ok)
	}
}

func TestStatusBarHintAt(t *testing.T) {
	sb := NewStatusBar(200, "1.0")
	sb.SetKeyHints([]StatusHintInput{{Key: "/", Desc: "search"}, {Key: "Enter", Desc: "focus"}, {Key: "K", Desc: "kill"}})
	// Layout: "[ / ] search" (10) + " · " (3) + "[ ⏎ ] focus" (9) + ...
	// The key badge has 1 cell of padding on each side; Enter draws as ⏎ but
	// still reports its key name.
	tests := []struct {
		x    int
		want string
		ok   bool
	}{
		{0, "/", true},
		{9, "/", true}, // end of "search"
		{11, "", false},
		{13, "Enter", true},
		{21, "Enter", true},
		{22, "", false},
		{25, "K", true},
		{150, "", false},
		{-1, "", false},
	}
	for _, tt := range tests {
		got, ok := sb.HintAt(tt.x)
		if ok != tt.ok || got != tt.want {
			t.Errorf("HintAt(%d) = %q,%v want %q,%v", tt.x, got, ok, tt.want, tt.ok)
		}
	}

	sb.SetError("boom")
	if _, ok := sb.HintAt(0); ok {
		t.Error("hints must not be clickable while an error replaces them")
	}
}

func TestPickerRowAt(t *testing.T) {
	sp := NewSessionPicker()
	sp.SetSize(100, 40)
	sp.Show([]string{"a", "b", "c"}, "")
	for y, want := range map[int]int{5: 0, 6: 1, 7: 2} {
		if got, ok := sp.RowAt(y); !ok || got != want {
			t.Errorf("session picker RowAt(%d) = %d,%v want %d", y, got, ok, want)
		}
	}
	if _, ok := sp.RowAt(4); ok {
		t.Error("separator row should not be selectable")
	}
	if _, ok := sp.RowAt(8); ok {
		t.Error("row past the list should not be selectable")
	}

	tp := NewThemePicker("")
	if got, ok := tp.RowAt(themePickerFirstRow); !ok || got != 0 {
		t.Errorf("theme picker first row = %d,%v", got, ok)
	}
	if _, ok := tp.RowAt(themePickerFirstRow - 1); ok {
		t.Error("blank line above themes should not be selectable")
	}
}

// On a very narrow pane the rows still start right below the one-line title.
func TestSidebarHitTestNarrowHeader(t *testing.T) {
	sb := hitSidebar(20)
	sb.SetSize(19, 20)
	sb.SetBreathingFrame(0) // re-render rows at the new width
	lines := strings.Split(sb.View(), "\n")
	for y, line := range lines {
		if strings.Contains(line, "alpha") {
			checkHit(t, sb.HitTest(3, y), wantHit{HitGroup, "alpha", 0})
			checkHit(t, sb.HitTest(3, y-1), wantHit{kind: HitNone})
			return
		}
	}
	t.Fatal("alpha header not rendered")
}
