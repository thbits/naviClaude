package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
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

// nameSource searches only project names (higher weight).
type nameSource []*session.Session

func (s nameSource) String(i int) string { return s[i].ProjectName }
func (s nameSource) Len() int            { return len(s) }

// fullSource searches all fields (lower weight fallback).
type fullSource []*session.Session

func (s fullSource) String(i int) string {
	sess := s[i]
	return strings.Join([]string{
		sess.ProjectName,
		sess.CWD,
		sess.GitBranch,
		sess.Summary,
		sess.TmuxSession,
	}, " ")
}

func (s fullSource) Len() int { return len(s) }

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

// Update handles text input only. Navigation (up/down) and actions (enter/esc)
// are handled by the app, which uses the sidebar for cursor management.
func (m SearchModel) Update(msg tea.Msg) (SearchModel, tea.Cmd) {
	if !m.active {
		return m, nil
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

	// Scoring strategy:
	//   3000  -- fuzzy match on project name (typo-tolerant, highest priority)
	//   2000  -- case-insensitive substring match on summary (reliable content search)
	//    500  -- case-insensitive substring match on CWD / git branch / tmux session
	//   fuzzy score (< 500) -- fuzzy fallback on full concatenated fields
	type scored struct {
		session *session.Session
		score   int
	}

	lower := strings.ToLower(query)
	bestScore := make(map[int]int) // session index -> best weighted score

	// Fuzzy match on project name (highest weight, typo-tolerant).
	nameMatches := fuzzy.FindFrom(query, nameSource(m.sessions))
	for _, match := range nameMatches {
		s := 3000 + match.Score
		if s > bestScore[match.Index] {
			bestScore[match.Index] = s
		}
	}

	// Substring match on summary -- reliable for keyword content search.
	for i, sess := range m.sessions {
		if sess.Summary != "" && strings.Contains(strings.ToLower(sess.Summary), lower) {
			s := 2000
			if s > bestScore[i] {
				bestScore[i] = s
			}
		}
	}

	// Substring match on other fields: CWD, git branch, tmux session name.
	for i, sess := range m.sessions {
		fields := []string{sess.CWD, sess.GitBranch, sess.TmuxSession}
		for _, f := range fields {
			if f != "" && strings.Contains(strings.ToLower(f), lower) {
				s := 500
				if s > bestScore[i] {
					bestScore[i] = s
				}
				break
			}
		}
	}

	// Fuzzy fallback on the full concatenated string (catches partial matches
	// not covered by substring search above, e.g. typos in CWD).
	fullMatches := fuzzy.FindFrom(query, fullSource(m.sessions))
	for _, match := range fullMatches {
		if match.Score > bestScore[match.Index] {
			bestScore[match.Index] = match.Score
		}
	}

	var results []scored
	for idx, score := range bestScore {
		results = append(results, scored{session: m.sessions[idx], score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	m.results = make([]*session.Session, len(results))
	for i, r := range results {
		m.results[i] = r.session
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
