package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/thbits/naviClaude/internal/styles"
)

// SessionPickerModel is an overlay choosing which tmux session a closed session
// resumes in, offering a "create new" row when the query names no existing one.
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

// SetTitle sets the popup title, e.g. "Move to session".
func (m *SessionPickerModel) SetTitle(title string) { m.title = title }

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

// rows returns the displayed rows: substring matches then the create-new row, or,
// with no substring match, the create-new row above loose fuzzy matches.
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

// runFilter re-applies the filter and resets the cursor to the top so a quick
// Enter picks the best match (or create-new).
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

// filterSessions keeps names containing query (case-insensitive), in order. It is
// stricter than fuzzy matching so loosely related names don't crowd out create-new.
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

// looseMatches returns ranked fuzzy matches, the fallback when nothing matches as
// a substring, so abbreviations like "nvc" -> "naviclaude" stay reachable.
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

// sessionPickerChromeRows is the popup rows taken by non-candidate chrome: border (2),
// padding (2), title, input, two separators (2), footer.
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

// window returns the [start, end) slice of n rows shown around the cursor.
func (m SessionPickerModel) window(n int) (int, int) {
	max := m.visibleRows()
	start := 0
	if m.cursor >= max {
		start = m.cursor - max + 1
	}
	end := start + max
	if end > n {
		end = n
	}
	return start, end
}

// sessionPickerFirstRow is the popup-local row of the first candidate: border
// (1) + padding (1) + title, input, separator (3).
const sessionPickerFirstRow = 5

// RowAt returns the row index drawn at popup-local row y, skipping the
// "similar" divider View draws before the first loose match.
func (m SessionPickerModel) RowAt(y int) (int, bool) {
	start, end := m.window(len(m.rows()))
	looseAt := m.firstLooseRow()
	line := sessionPickerFirstRow
	for i := start; i < end; i++ {
		if i == looseAt {
			line++
		}
		if line == y {
			return i, true
		}
		line++
	}
	return 0, false
}

// Cursor returns the highlighted row index.
func (m SessionPickerModel) Cursor() int { return m.cursor }

// SetCursor highlights row i.
func (m *SessionPickerModel) SetCursor(i int) {
	if i >= 0 && i < len(m.rows()) {
		m.cursor = i
	}
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
		start, end := m.window(len(rows))
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
