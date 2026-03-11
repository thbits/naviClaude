package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/tomhalo/naviclaude/internal/session"
	"github.com/tomhalo/naviclaude/internal/styles"
)

// SearchModel provides fuzzy search over sessions.
type SearchModel struct {
	input    textinput.Model
	sessions []*session.Session
	results  []*session.Session
	cursor   int
	active   bool
	width    int
	height   int
}

// sessionSource implements fuzzy.Source for the session list.
type sessionSource []*session.Session

func (s sessionSource) String(i int) string {
	sess := s[i]
	return strings.Join([]string{
		sess.ProjectName,
		sess.CWD,
		sess.GitBranch,
		sess.Summary,
	}, " ")
}

func (s sessionSource) Len() int {
	return len(s)
}

// NewSearch creates a SearchModel.
func NewSearch() SearchModel {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.PromptStyle = styles.SearchPrompt
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)
	ti.Placeholder = "search sessions..."
	ti.CharLimit = 128
	return SearchModel{
		input: ti,
	}
}

// SetSessions sets the full searchable session list.
func (m *SearchModel) SetSessions(sessions []*session.Session) {
	m.sessions = sessions
	if m.active {
		m.runSearch()
	}
}

// Activate shows the search input and focuses it.
func (m *SearchModel) Activate() {
	m.active = true
	m.input.SetValue("")
	m.input.Focus()
	m.cursor = 0
	m.results = m.sessions // show all before typing
}

// Deactivate clears and hides the search.
func (m *SearchModel) Deactivate() {
	m.active = false
	m.input.SetValue("")
	m.input.Blur()
	m.results = nil
	m.cursor = 0
}

// IsActive returns whether search is active.
func (m *SearchModel) IsActive() bool {
	return m.active
}

// Results returns the filtered session list.
func (m *SearchModel) Results() []*session.Session {
	return m.results
}

// SelectedResult returns the session at the current cursor in results.
func (m *SearchModel) SelectedResult() *session.Session {
	if m.cursor < 0 || m.cursor >= len(m.results) {
		return nil
	}
	return m.results[m.cursor]
}

// SetSize updates the search component dimensions.
func (m *SearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.input.Width = w - 6 // account for prompt and padding
}

// Init satisfies the tea.Model interface.
func (m SearchModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles typing, navigation within results, and Esc to cancel.
func (m SearchModel) Update(msg tea.Msg) (SearchModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.Deactivate()
			return m, nil
		case "down", "ctrl+n":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return m, nil
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "enter":
			// The main app handles selection via SelectedResult().
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.runSearch()
	return m, cmd
}

func (m *SearchModel) runSearch() {
	query := m.input.Value()
	if query == "" {
		m.results = m.sessions
		m.cursor = 0
		return
	}

	source := sessionSource(m.sessions)
	matches := fuzzy.FindFrom(query, source)

	m.results = make([]*session.Session, len(matches))
	for i, match := range matches {
		m.results[i] = m.sessions[match.Index]
	}

	if m.cursor >= len(m.results) {
		m.cursor = len(m.results) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// View renders the search input bar.
func (m SearchModel) View() string {
	if !m.active {
		return ""
	}

	inputView := m.input.View()
	return styles.SearchInput.Width(m.width - 4).Render(inputView)
}
