package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestReboundFocusKeyFocusesSession(t *testing.T) {
	m, _ := clickModel(t)
	m.keys = KeyMapFromConfig(config.KeyBindings{Focus: "o"})
	if !m.sidebar.SelectByID("a1") {
		t.Fatal("precondition: a1 not selectable")
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = next.(Model)
	if m.mode != ModeList {
		t.Fatalf("enter with focus rebound: mode = %v, want %v", m.mode, ModeList)
	}

	next, _ = m.handleKey(runeKey("o"))
	*m = next.(Model)
	if m.mode != ModePassthrough {
		t.Fatalf("rebound focus key: mode = %v, want %v", m.mode, ModePassthrough)
	}
}

func TestReboundFocusKeyTogglesGroupHeader(t *testing.T) {
	m, _ := clickModel(t)
	m.keys = KeyMapFromConfig(config.KeyBindings{Focus: "o"})
	m.sidebar.SetCursor(0) // the "zzmouse" header
	before := len(m.sidebar.FlatItems())

	next, _ := m.handleKey(runeKey("o"))
	*m = next.(Model)
	if got := len(m.sidebar.FlatItems()); got != before-2 {
		t.Fatalf("flat items = %d, want %d (rebound focus key should collapse the header)", got, before-2)
	}
}

func TestDoubleClickFocusesWithReboundFocusKey(t *testing.T) {
	m, clk := clickModel(t)
	m.keys = KeyMapFromConfig(config.KeyBindings{Focus: "o"})
	press(m, tea.MouseButtonLeft, xList, yTwo)
	release(m, xList, yTwo)
	*clk = clk.Add(150 * time.Millisecond)
	press(m, tea.MouseButtonLeft, xList, rowOf(t, m, "a2"))
	if m.mode != ModePassthrough {
		t.Fatalf("double-click: mode = %v, want %v", m.mode, ModePassthrough)
	}
}

func TestFocusKeyLabels(t *testing.T) {
	if got := DefaultKeyMap().StatusHints()[0].Key; got != "Enter" {
		t.Errorf("default focus hint = %q, want Enter", got)
	}
	if got := KeyMapFromConfig(config.KeyBindings{Focus: "o"}).StatusHints()[0].Key; got != "o" {
		t.Errorf("rebound focus hint = %q, want o", got)
	}
}
