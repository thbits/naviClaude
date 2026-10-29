package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// Sections a group's rows sort into, top to bottom; the sort order applies
// within each one.
const (
	sectionPinned = iota
	sectionWaiting
	sectionDone
	sectionWorking
	sectionIdle
)

var sectionLabels = [...]string{"pinned", "waiting", "done", "working", "idle"}

// sectionOf places a session: pinned whatever its status, then waiting on the
// user, then finished unseen, then working (background work included), then the rest.
func (m *SidebarModel) sectionOf(s *session.Session) int {
	status := session.EffectiveStatus(s, m.background[s.ID])
	switch {
	case m.IsPinned(s):
		return sectionPinned
	case needsUser(s):
		return sectionWaiting
	case status == session.StatusIdle && s.Attention == attention.Done:
		return sectionDone
	case status == session.StatusActive:
		return sectionWorking
	}
	return sectionIdle
}

// SetPinned sets the keys of the sessions pinned to the top of their group.
func (m *SidebarModel) SetPinned(keys []string) { m.pinned = keys }

// Pinned returns the keys of the pinned sessions.
func (m *SidebarModel) Pinned() []string { return m.pinned }

// pinKey is what a pin on s is stored under: its session key, or its tmux pane
// until the session has an ID.
func pinKey(s *session.Session) string {
	if k := s.Key(); k != "" {
		return k
	}
	return s.TmuxPaneID
}

// IsPinned reports whether s is pinned to the top of its group.
func (m *SidebarModel) IsPinned(s *session.Session) bool {
	if s == nil {
		return false
	}
	return slices.ContainsFunc(m.pinned, func(k string) bool {
		return k != "" && (k == s.Key() || k == s.TmuxPaneID)
	})
}

// TogglePin pins or unpins s and re-sorts, reporting whether it is now pinned.
func (m *SidebarModel) TogglePin(s *session.Session) bool {
	if s == nil || pinKey(s) == "" {
		return false
	}
	pinned := !m.IsPinned(s)
	if pinned {
		m.pinned = append(slices.Clone(m.pinned), pinKey(s))
	} else {
		m.pinned = slices.DeleteFunc(slices.Clone(m.pinned), func(k string) bool {
			return k == s.Key() || k == s.TmuxPaneID
		})
	}
	m.rebuildGroups()
	return pinned
}

// rekeyPins moves pins held by a tmux pane onto the session ID once it is known,
// so they survive a tmux restart.
func (m *SidebarModel) rekeyPins() {
	for _, s := range m.Sessions {
		if s.Key() == "" || s.TmuxPaneID == "" {
			continue
		}
		if i := slices.Index(m.pinned, s.TmuxPaneID); i >= 0 {
			m.pinned = slices.Clone(m.pinned)
			m.pinned[i] = s.Key()
		}
	}
}

// sameSection reports whether src may land next to anchor: within one group a
// row only moves among its own section, since the sections decide the rest.
func (m *SidebarModel) sameSection(src, anchor *session.Session) bool {
	if src == nil || anchor == nil || src.TmuxSession != anchor.TmuxSession {
		return true
	}
	return m.sections[src] == m.sections[anchor]
}

// renderSectionLine is the one-row break above a section: a solid rule across
// the pane with the label set into it, e.g. "─ working ─────".
func (m SidebarModel) renderSectionLine(section int) string {
	line := lipgloss.NewStyle().Foreground(styles.ColorBorder)
	label := " " + sectionLabels[section] + " "
	rest := max(m.width-1-lipgloss.Width(label), 1)
	return line.Render("─") + lipgloss.NewStyle().Foreground(styles.ColorDimText).Render(label) +
		line.Render(strings.Repeat("─", rest))
}
