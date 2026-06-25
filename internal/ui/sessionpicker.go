package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/thbits/naviClaude/internal/styles"
)

// SessionPickerModel is an overlay that chooses where a closed session is
// resumed. It lists the live tmux sessions (recency-ordered, supplied by the
// caller) and fuzzy-filters them as the user types. When the typed text does
// not exactly match an existing session, a synthetic "create new" row is
// offered so the user can spin up a fresh tmux session by name. Selecting a row
// yields its name and whether it is the create-new row.
type SessionPickerModel struct {
	input    textinput.Model
	sessions []string // all candidate session names, recency-ordered
	filtered []string // current fuzzy-filtered subset
	cursor   int
	visible  bool
	width    int
	height   int
	title    string
}

// NewSessionPicker creates a SessionPickerModel.
func NewSessionPicker() SessionPickerModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = styles.SearchPrompt
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)
	ti.Placeholder = "filter sessions or type a new name ..."
	ti.CharLimit = 256
	return SessionPickerModel{
		input: ti,
		title: "Resume into session",
	}
}

// Show opens the picker over the given session names and pre-highlights
// current (the tmux session naviClaude is running in) when it is present.
func (m *SessionPickerModel) Show(sessions []string, current string) {
	m.visible = true
	m.sessions = sessions
	m.input.SetValue("")
	m.input.Focus()
	m.filtered = filterSessions("", sessions)
	m.cursor = 0
	for i, s := range m.filtered {
		if s == current {
			m.cursor = i
			break
		}
	}
}

// Hide closes the picker and clears its state.
func (m *SessionPickerModel) Hide() {
	m.visible = false
	m.input.SetValue("")
	m.input.Blur()
	m.sessions = nil
	m.filtered = nil
	m.cursor = 0
}

// IsVisible reports whether the picker is open.
func (m *SessionPickerModel) IsVisible() bool { return m.visible }

// SetSize updates the popup container dimensions.
func (m *SessionPickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if w > 8 {
		m.input.Width = w - 8
	}
}

// MoveUp / MoveDown move the cursor within the displayed rows.
func (m *SessionPickerModel) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *SessionPickerModel) MoveDown() {
	if m.cursor < len(m.rows())-1 {
		m.cursor++
	}
}

// sessionRow is one selectable row: an existing session (loose=false for a
// substring match, loose=true for a fuzzy fallback match) or the create-new row.
type sessionRow struct {
	name  string
	isNew bool
	loose bool // true for a fuzzy fallback match shown below the create-new row
}

// rows returns the displayed rows. With no query, every session is listed. With
// a query, substring matches come first (the create-new row trailing them);
// when nothing matches as a substring, the create-new row floats to the top and
// loose fuzzy matches are listed below it as a fallback.
func (m SessionPickerModel) rows() []sessionRow {
	query := strings.TrimSpace(m.input.Value())
	if query == "" {
		rows := make([]sessionRow, 0, len(m.sessions))
		for _, s := range m.sessions {
			rows = append(rows, sessionRow{name: s})
		}
		return rows
	}

	newName := newSessionName(query, m.sessions)

	// m.filtered holds the substring matches (see runFilter).
	if len(m.filtered) > 0 {
		rows := make([]sessionRow, 0, len(m.filtered)+1)
		for _, s := range m.filtered {
			rows = append(rows, sessionRow{name: s})
		}
		if newName != "" {
			rows = append(rows, sessionRow{name: newName, isNew: true})
		}
		return rows
	}

	// No substring match: create-new on top, loose fuzzy matches below.
	var rows []sessionRow
	if newName != "" {
		rows = append(rows, sessionRow{name: newName, isNew: true})
	}
	for _, s := range looseMatches(query, m.sessions) {
		rows = append(rows, sessionRow{name: s, loose: true})
	}
	return rows
}

// firstLooseRow returns the index of the first loose (fuzzy fallback) row, or
// -1 when there is none. The View draws a "similar" separator before it.
func (m SessionPickerModel) firstLooseRow() int {
	for i, r := range m.rows() {
		if r.loose {
			return i
		}
	}
	return -1
}

// Selected returns the highlighted row's name and whether it is the create-new
// row. Returns ("", false) when there is nothing to select.
func (m SessionPickerModel) Selected() (string, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return "", false
	}
	r := rows[m.cursor]
	return r.name, r.isNew
}

// runFilter re-applies the filter for the current query. The cursor resets to
// the top row so a quick Enter after typing selects the most sensible default
// (the best match, or the create-new row when nothing matches).
func (m *SessionPickerModel) runFilter() {
	m.filtered = filterSessions(m.input.Value(), m.sessions)
	m.cursor = 0
}

// Init satisfies tea.Model.
func (m SessionPickerModel) Init() tea.Cmd { return textinput.Blink }

// Update feeds text-input events (typing) and re-filters. Navigation and
// selection keys are handled by the app, which calls the methods above.
func (m SessionPickerModel) Update(msg tea.Msg) (SessionPickerModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.runFilter()
	return m, cmd
}

// ---------------------------------------------------------------------------
// Pure logic (unit-tested directly)
// ---------------------------------------------------------------------------

// filterSessions filters session names to those containing query as a
// case-insensitive substring, preserving the caller's recency order. An empty
// (whitespace) query returns the names unchanged. Substring matching is
// deliberately stricter than subsequence fuzzy matching: it only surfaces
// names that actually contain what was typed, so loosely-related names don't
// crowd out the create-new option.
func filterSessions(query string, sessions []string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return sessions
	}
	var out []string
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s), q) {
			out = append(out, s)
		}
	}
	return out
}

// stringSource adapts a slice of session names for sahilm/fuzzy.
type stringSource []string

func (n stringSource) String(i int) string { return n[i] }
func (n stringSource) Len() int            { return len(n) }

// looseMatches returns subsequence ("fuzzy") matches for query, ranked by the
// matcher's score. It is the hybrid fallback shown only when no name contains
// the query as a substring, so abbreviations (e.g. "nvc" -> "naviclaude") stay
// reachable without polluting normal substring results.
func looseMatches(query string, sessions []string) []string {
	matches := fuzzy.FindFrom(query, stringSource(sessions))
	out := make([]string, len(matches))
	for i, mt := range matches {
		out[i] = sessions[mt.Index]
	}
	return out
}

// newSessionName returns the trimmed query when it names a session to create:
// non-empty and not an exact match of an existing session. Otherwise "".
func newSessionName(query string, sessions []string) string {
	name := strings.TrimSpace(query)
	if name == "" {
		return ""
	}
	for _, s := range sessions {
		if s == name {
			return ""
		}
	}
	return name
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// sessionPickerChromeRows is the number of popup rows consumed by non-candidate
// chrome: border top+bottom (2) + padding top+bottom (2) + title (1) + input
// (1) + two separators (2) + footer (1) = 9 rows.
const sessionPickerChromeRows = 9

func (m SessionPickerModel) visibleRows() int {
	rows := m.height - sessionPickerChromeRows
	if rows < 3 {
		rows = 3
	}
	if rows > 20 {
		rows = 20
	}
	return rows
}

// View renders the picker popup (placement is done by the caller via
// PlaceOverlay).
func (m SessionPickerModel) View() string {
	if !m.visible {
		return ""
	}

	boxWidth := m.width / 2
	if boxWidth < 40 {
		boxWidth = 40
	}

	title := lipgloss.NewStyle().Foreground(styles.ColorPurple).Bold(true).Render(m.title)
	sep := lipgloss.NewStyle().Foreground(styles.ColorGray).
		Render(strings.Repeat("─", boxWidth))

	var lines []string
	lines = append(lines, title)
	lines = append(lines, m.input.View())
	lines = append(lines, sep)

	rows := m.rows()
	if len(rows) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.ColorGray).
			Render("  (no tmux sessions -- type a name to create one)"))
	} else {
		max := m.visibleRows()
		start := 0
		if m.cursor >= max {
			start = m.cursor - max + 1
		}
		end := start + max
		if end > len(rows) {
			end = len(rows)
		}
		looseAt := m.firstLooseRow()
		for i := start; i < end; i++ {
			// Divider before the loose (fuzzy fallback) section.
			if i == looseAt {
				lines = append(lines, lipgloss.NewStyle().Foreground(styles.ColorGray).
					Render("  ── similar ──"))
			}
			r := rows[i]
			var label string
			if r.isNew {
				marker := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("+")
				label = marker + " new session: " + r.name
			} else {
				marker := lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("@")
				label = marker + " " + r.name
			}
			if i == m.cursor {
				lines = append(lines, lipgloss.NewStyle().
					Foreground(styles.ColorBlue).
					Background(styles.ColorSelection).
					Bold(true).
					PaddingLeft(1).PaddingRight(1).
					Render(label))
			} else {
				lines = append(lines, lipgloss.NewStyle().
					Foreground(styles.ColorFg).
					PaddingLeft(1).PaddingRight(1).
					Render(label))
			}
		}
	}

	lines = append(lines, sep)
	footer := lipgloss.NewStyle().Foreground(styles.ColorGray).
		Render("type filter  ↑↓ move  Enter resume  Esc cancel")
	lines = append(lines, footer)

	content := strings.Join(lines, "\n")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorPurple).
		Background(styles.ColorBgPanel).
		Padding(1, 2)
	return border.Render(content)
}
