package app

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// motionModel is a list of 9 rows: header aa, s1-s4, header bb, s5-s7.
func motionModel(t *testing.T) *Model {
	t.Helper()
	now := time.Now()
	var sessions []*session.Session
	for i := 1; i <= 7; i++ {
		group := "aa"
		if i > 4 {
			group = "bb"
		}
		sessions = append(sessions, &session.Session{
			ID: fmt.Sprintf("s%d", i), ProjectName: fmt.Sprintf("name%d", i), TmuxSession: group,
			TmuxTarget: fmt.Sprintf("%s:%d.0", group, i), Status: session.StatusIdle,
			LastActivity: now.Add(-time.Duration(i) * time.Second),
		})
	}
	m := newFocusModel(0, sessions)
	m.keys = DefaultKeyMap()
	m.search = ui.NewSearch()
	m.allSessions = sessions
	m.mode = ModeList
	if m.sidebar.Cursor() != 0 {
		m.sidebar.SetCursor(0)
	}
	return &m
}

func typeKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "down", "up", "esc":
			msg = map[string]tea.KeyMsg{
				"down": {Type: tea.KeyDown}, "up": {Type: tea.KeyUp}, "esc": {Type: tea.KeyEsc},
			}[k]
		default:
			msg = runeKey(k)
		}
		next, _ := m.handleKey(msg)
		*m = next.(Model)
	}
}

func wantCursor(t *testing.T, m *Model, want int) {
	t.Helper()
	if got := m.sidebar.Cursor(); got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
}

func TestCountedJKMoveThatManySessions(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "5", "j") // s5, past header bb
	wantCursor(t, m, 6)
	typeKeys(t, m, "2", "k")
	wantCursor(t, m, 3)
	typeKeys(t, m, "3", "down")
	wantCursor(t, m, 7)
	typeKeys(t, m, "2", "0", "j") // past the end clamps to the last session
	wantCursor(t, m, 8)
	typeKeys(t, m, "k", "k") // a bare k still stops on header bb
	wantCursor(t, m, 6)
	typeKeys(t, m, "k")
	wantCursor(t, m, 5)
	if m.count != 0 {
		t.Errorf("count = %d after its motion, want 0", m.count)
	}
}

func TestPendingCountShowsInStatusBar(t *testing.T) {
	m := motionModel(t)
	m.statusbar.SetSize(80)
	plain := m.statusbar.View()
	typeKeys(t, m, "1", "2")
	if m.count != 12 {
		t.Fatalf("count = %d, want 12", m.count)
	}
	if m.statusbar.View() == plain {
		t.Error("status bar should show the pending count")
	}
	typeKeys(t, m, "j")
	if m.statusbar.View() != plain {
		t.Error("status bar should drop the count once used")
	}
}

func TestBareZeroIsNotACount(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "0", "j")
	wantCursor(t, m, 1)
	typeKeys(t, m, "1", "0", "G") // 0 extends an open count; session 10 clamps to the last
	wantCursor(t, m, 8)
}

func TestNonMotionKeyDropsCount(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "3", "esc", "j")
	wantCursor(t, m, 1)
}

func TestMouseClickDropsCount(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "4")
	next, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 500, Y: 500})
	*m = next.(Model)
	typeKeys(t, m, "j")
	wantCursor(t, m, 1)
}

func TestCountedGGoesToSession(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "3", "G")
	wantCursor(t, m, 3)
	typeKeys(t, m, "G")
	wantCursor(t, m, 8)
	typeKeys(t, m, "g")
	wantCursor(t, m, 0)
	typeKeys(t, m, "5", "g") // s5 sits under header bb
	wantCursor(t, m, 6)
}

func TestToggleLineNumbersSavesConfig(t *testing.T) {
	m := motionModel(t)
	var saved []string
	m.saveConfig = func(c config.Config) error { saved = append(saved, c.LineNumbers); return nil }
	m.cfg.LineNumbers = "off"
	typeKeys(t, m, "#")
	typeKeys(t, m, "#")
	m.lineNumbersOn = "absolute" // as when the config started with absolute
	typeKeys(t, m, "#")
	want := []string{"relative", "off", "absolute"}
	if fmt.Sprint(saved) != fmt.Sprint(want) {
		t.Errorf("saved modes %v, want %v", saved, want)
	}
}

func TestBracesJumpBetweenGroupHeaders(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "}")
	wantCursor(t, m, 5)
	typeKeys(t, m, "}") // no header below: the last row
	wantCursor(t, m, 8)
	typeKeys(t, m, "{")
	wantCursor(t, m, 5)
	typeKeys(t, m, "j", "j", "2", "{") // its own header, then the one above
	wantCursor(t, m, 0)
	typeKeys(t, m, "2", "}")
	wantCursor(t, m, 8)
}

func TestCountedMoveDefersOneSync(t *testing.T) {
	m := motionModel(t)
	gen := m.selectionGen
	typeKeys(t, m, "5", "j")
	if m.selectionGen != gen+1 || !m.selectionPending {
		t.Errorf("selectionGen %d -> %d, pending %v; want one deferred sync", gen, m.selectionGen, m.selectionPending)
	}
}

func TestDigitBoundToActionKeepsIt(t *testing.T) {
	m := motionModel(t)
	m.keys.Stats = "5"
	typeKeys(t, m, "5")
	if m.count != 0 || m.mode != ModeStats {
		t.Errorf("count = %d mode = %v, want the stats binding to win", m.count, m.mode)
	}
}

func TestDigitsTypeIntoSearch(t *testing.T) {
	m := motionModel(t)
	typeKeys(t, m, "/", "5")
	if m.count != 0 {
		t.Fatalf("count = %d in search, want digits typed into the query", m.count)
	}
	if got := len(m.search.Results()); got != 1 {
		t.Errorf("search for 5 found %d results, want 1 (name5)", got)
	}
}
