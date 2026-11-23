package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/styles"
)

// textMatch is one search hit: a content line and its [from, to) display columns.
type textMatch struct{ line, from, to int }

// previewSearch is a text search over what the preview holds.
type previewSearch struct {
	input   textinput.Model
	typing  bool // the query input is open
	query   string
	matches []textMatch // in reading order, over the width-truncated lines
	current int

	// Scroll state when the search opened: the anchor matches are looked up from
	// while typing, and where clearing the search returns to.
	origin         int
	originScrolled bool
}

// OpenSearch starts a new search with an empty query input.
func (m *PreviewModel) OpenSearch() {
	m.ClearSearch()
	ti := textinput.New()
	ti.Prompt = "find: "
	ti.PromptStyle = styles.SearchPrompt
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)
	ti.CharLimit = 128
	ti.Cursor.SetMode(cursor.CursorStatic) // no blink messages to route
	ti.Width = m.searchInputWidth()
	ti.Focus()
	m.search = previewSearch{input: ti, typing: true, origin: m.viewport.YOffset, originScrolled: m.userScrolled}
}

// searchCounterRoom is the header columns kept for the counter ("no matches").
const searchCounterRoom = 11

// searchInputWidth is how much of the query the header can show at once.
func (m *PreviewModel) searchInputWidth() int {
	// Header width less its padding, the "find: " prompt and the cursor cell.
	return max(m.width-3-2-len("find: ")-1-searchCounterRoom, 4)
}

// SearchTyping reports whether the query input is open.
func (m *PreviewModel) SearchTyping() bool { return m.search.typing }

// HasSearch reports whether a search is open or its matches are still highlighted.
func (m *PreviewModel) HasSearch() bool { return m.search.typing || m.search.query != "" }

// SearchCount returns the current match's 1-based position and the match count.
func (m *PreviewModel) SearchCount() (current, total int) {
	if len(m.search.matches) == 0 {
		return 0, 0
	}
	return m.search.current + 1, len(m.search.matches)
}

// TypeSearch feeds a key to the query input and jumps to the first match at or
// below where the preview stood when the search opened.
func (m *PreviewModel) TypeSearch(msg tea.KeyMsg) {
	if !m.search.typing {
		return
	}
	m.search.input, _ = m.search.input.Update(msg)
	q := m.search.input.Value()
	if q == m.search.query {
		return
	}
	m.search.query = q
	m.refreshViewport()
	m.search.current = 0
	for i, mt := range m.search.matches {
		if mt.line >= m.search.origin {
			m.search.current = i
			break
		}
	}
	m.showMatch()
}

// CommitSearch closes the query input, keeping the matches highlighted; it
// reports false when the query was empty and the search is gone.
func (m *PreviewModel) CommitSearch() bool {
	if !m.search.typing {
		return m.search.query != ""
	}
	m.search.typing = false
	m.search.input.Blur()
	if m.search.query == "" {
		m.ClearSearch()
		return false
	}
	return true
}

// StepMatch moves to the next (delta > 0) or previous match, wrapping around.
func (m *PreviewModel) StepMatch(delta int) {
	n := len(m.search.matches)
	if n == 0 {
		return
	}
	m.search.current = ((m.search.current+delta)%n + n) % n
	m.showMatch()
}

// ClearSearch drops the search and its highlights and puts the preview back
// where it was when the search opened.
func (m *PreviewModel) ClearSearch() {
	if !m.HasSearch() {
		return
	}
	origin, scrolled := m.search.origin, m.search.originScrolled
	m.search = previewSearch{}
	m.refreshViewport()
	m.restoreScroll(origin, scrolled)
}

func (m *PreviewModel) restoreScroll(offset int, scrolled bool) {
	m.userScrolled = scrolled
	if scrolled {
		m.viewport.SetYOffset(offset)
	} else {
		m.viewport.GotoBottom()
	}
}

// showMatch repaints for the current match and scrolls it into view; with no
// match the preview goes back to where the search opened.
func (m *PreviewModel) showMatch() {
	m.refreshViewport()
	if len(m.search.matches) == 0 {
		m.restoreScroll(m.search.origin, m.search.originScrolled)
		return
	}
	line := m.search.matches[m.search.current].line
	if line < m.viewport.YOffset || line >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(line - m.viewport.Height/2)
	}
	// Keeps the next capture from snapping back to the bottom.
	m.userScrolled = !m.atBottom()
}

// refreshViewport hands the viewport the content cut to its width, with the
// search matches recomputed and painted.
func (m *PreviewModel) refreshViewport() {
	body := m.truncateToWidth(m.content)
	if m.search.query == "" {
		m.search.matches, m.search.current = nil, 0
		m.viewport.SetContent(body)
		return
	}
	lines := strings.Split(body, "\n")
	m.search.matches = findMatches(lines, m.search.query)
	m.search.current = max(min(m.search.current, len(m.search.matches)-1), 0)
	for i := 0; i < len(m.search.matches); {
		// Matches are in line order, so one pass paints each line's run.
		j := i
		for j < len(m.search.matches) && m.search.matches[j].line == m.search.matches[i].line {
			j++
		}
		line := m.search.matches[i].line
		lines[line] = paintMatches(lines[line], m.search.matches[i:j], m.search.current-i)
		i = j
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
}

// findMatches returns every non-overlapping match of query in lines, smart-case:
// case matters only when the query has an upper-case letter.
func findMatches(lines []string, query string) []textMatch {
	q := []rune(query)
	if len(q) == 0 {
		return nil
	}
	fold := !strings.ContainsFunc(query, unicode.IsUpper)
	var out []textMatch
	for li, line := range lines {
		// Rune-wise, since lower-casing a string can change its byte length.
		orig := []rune(ansi.Strip(line))
		if len(orig) < len(q) {
			continue
		}
		text := orig
		if fold {
			text = make([]rune, len(orig))
			for i, r := range orig {
				text[i] = unicode.ToLower(r)
			}
		}
		for i := 0; i+len(q) <= len(text); {
			if !slices.Equal(text[i:i+len(q)], q) {
				i++
				continue
			}
			from := ansi.StringWidth(string(orig[:i]))
			to := from + ansi.StringWidth(string(orig[i:i+len(q)]))
			if to > from {
				out = append(out, textMatch{line: li, from: from, to: to})
			}
			i += len(q)
		}
	}
	return out
}

// paintMatches highlights one line's matches, the current-th one distinctly,
// keeping the line's own colors around them.
func paintMatches(line string, matches []textMatch, current int) string {
	var b strings.Builder
	pos := 0
	for i, mt := range matches {
		style := styles.PreviewMatch
		if i == current {
			style = styles.PreviewMatchCurrent
		}
		b.WriteString(ansi.Cut(line, pos, mt.from))
		b.WriteString("\x1b[0m")
		b.WriteString(style.Render(ansi.Strip(ansi.Cut(line, mt.from, mt.to))))
		pos = mt.to
	}
	b.WriteString(ansi.Cut(line, pos, ansi.StringWidth(line)))
	return b.String()
}

// renderSearchBar is the header while a search is on: the query on the left and
// the match counter ("3/17") on the right.
func (m PreviewModel) renderSearchBar(maxWidth int) string {
	contentWidth := max(maxWidth-2, 1) // the header styles pad one column each side

	counter := ""
	if cur, total := m.SearchCount(); total > 0 {
		counter = lipgloss.NewStyle().Foreground(styles.ColorCyan).Bold(true).Render(fmt.Sprintf("%d/%d", cur, total))
	} else if m.search.query != "" {
		counter = lipgloss.NewStyle().Foreground(styles.ColorRed).Render("no matches")
	}

	style := styles.PreviewHeader
	room := max(contentWidth-lipgloss.Width(counter)-1, 1)
	var left string
	if m.search.typing {
		style = styles.PreviewHeaderFocused
		left = m.search.input.View()
	} else {
		left = styles.SearchPrompt.Render("find: ") + lipgloss.NewStyle().Foreground(styles.ColorFg).Render(m.search.query)
	}
	left = truncateDisplay(left, room)
	gap := max(contentWidth-lipgloss.Width(left)-lipgloss.Width(counter), 1)
	line := ansi.Truncate(left+strings.Repeat(" ", gap)+counter, contentWidth, "")
	return style.Width(maxWidth).Render(line)
}
