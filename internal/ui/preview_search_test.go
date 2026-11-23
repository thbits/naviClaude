package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// searchPreview is a 40x12 preview (viewport 38x10) holding content.
func searchPreview(content string) *PreviewModel {
	p := NewPreview(40, 12)
	p.SetSize(40, 12)
	p.SetContent(content)
	return &p
}

func searchFor(p *PreviewModel, q string) {
	p.OpenSearch()
	for _, r := range q {
		p.TypeSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func numbered(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	return strings.Join(lines, "\n")
}

// trueColor makes lipgloss emit real escapes, so highlights can be told apart.
func trueColor(t *testing.T) {
	t.Helper()
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	styles.ApplyTheme(styles.Named("tokyo-night")) // rebuild the styles under the new profile
	t.Cleanup(func() {
		lipgloss.SetColorProfile(orig)
		styles.ApplyTheme(styles.Named("tokyo-night"))
	})
}

func TestFindMatchesIsSmartCase(t *testing.T) {
	lines := []string{"Error: one", "no error here", "ERROR"}
	if got := len(findMatches(lines, "error")); got != 3 {
		t.Errorf("lower-case query matched %d lines, want all 3", got)
	}
	got := findMatches(lines, "Error")
	if len(got) != 1 || got[0] != (textMatch{line: 0, from: 0, to: 5}) {
		t.Errorf("mixed-case query matched %+v, want only line 0", got)
	}
	if got := findMatches(lines, ""); got != nil {
		t.Errorf("empty query matched %+v", got)
	}
}

func TestFindMatchesReportsEveryHitOnALine(t *testing.T) {
	got := findMatches([]string{"ab ab abab"}, "ab")
	want := []textMatch{{0, 0, 2}, {0, 3, 5}, {0, 6, 8}, {0, 8, 10}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("matches = %+v, want %+v", got, want)
	}
	// Overlapping hits are not double-counted.
	if got := findMatches([]string{"aaaa"}, "aa"); len(got) != 2 {
		t.Errorf("overlapping query gave %d matches, want 2", len(got))
	}
}

// Columns come from the text without its escapes, counting wide runes as two.
func TestFindMatchesMapsColorAndWideTextToColumns(t *testing.T) {
	colored := "\x1b[31mred\x1b[0m \x1b[1;32mgreen\x1b[0m word"
	if got := findMatches([]string{colored}, "green"); len(got) != 1 || got[0] != (textMatch{0, 4, 9}) {
		t.Errorf("colored line: matches = %+v, want columns 4-9", got)
	}
	// A match can straddle a color change.
	if got := findMatches([]string{colored}, "d g"); len(got) != 1 || got[0] != (textMatch{0, 2, 5}) {
		t.Errorf("match across escapes = %+v, want columns 2-5", got)
	}
	wide := "日本語 test 日本"
	if got := findMatches([]string{wide}, "test"); len(got) != 1 || got[0] != (textMatch{0, 7, 11}) {
		t.Errorf("after wide runes: matches = %+v, want columns 7-11", got)
	}
	if got := findMatches([]string{wide}, "本"); len(got) != 2 || got[0] != (textMatch{0, 2, 4}) || got[1] != (textMatch{0, 14, 16}) {
		t.Errorf("wide query: matches = %+v, want columns 2-4 and 14-16", got)
	}
	// Lower-casing must not shift later columns (İ lower-cases to two runes as a string).
	if got := findMatches([]string{"İİ x"}, "x"); len(got) != 1 || got[0] != (textMatch{0, 3, 4}) {
		t.Errorf("after İ: matches = %+v, want columns 3-4", got)
	}
}

func TestPaintMatchesKeepsTextWidthAndColors(t *testing.T) {
	trueColor(t)
	line := "\x1b[31mred apple\x1b[0m and 日本 apple pie"
	matches := findMatches([]string{line}, "apple")
	if len(matches) != 2 {
		t.Fatalf("matches = %+v, want 2", matches)
	}
	painted := paintMatches(line, matches, 1)
	if got, want := ansi.Strip(painted), ansi.Strip(line); got != want {
		t.Errorf("painted text = %q, want %q", got, want)
	}
	if got, want := ansi.StringWidth(painted), ansi.StringWidth(line); got != want {
		t.Errorf("painted width = %d, want %d", got, want)
	}
	other, current := styles.PreviewMatch.Render("apple"), styles.PreviewMatchCurrent.Render("apple")
	if other == current {
		t.Fatal("precondition: the current match must be styled apart from the others")
	}
	if strings.Count(painted, other) != 1 || strings.Count(painted, current) != 1 {
		t.Errorf("want one plain and one current highlight, got %q", painted)
	}
	if strings.Index(painted, other) > strings.Index(painted, current) {
		t.Errorf("the second match should be the current one: %q", painted)
	}
	if !strings.HasPrefix(painted, "\x1b[31mred ") {
		t.Errorf("text before the match lost its color: %q", painted)
	}
}

func TestPreviewSearchCountsAndWraps(t *testing.T) {
	p := searchPreview("alpha\nbeta alpha\ngamma\nalpha")
	searchFor(p, "alpha")
	if cur, total := p.SearchCount(); cur != 1 || total != 3 {
		t.Fatalf("count = %d/%d, want 1/3", cur, total)
	}
	if !p.CommitSearch() || p.SearchTyping() || !p.HasSearch() {
		t.Fatalf("after Enter: typing = %v, has search = %v; want the matches kept", p.SearchTyping(), p.HasSearch())
	}
	var seen []int
	for _, delta := range []int{1, 1, 1, -1, -1, -1} {
		p.StepMatch(delta)
		cur, _ := p.SearchCount()
		seen = append(seen, cur)
	}
	if got, want := fmt.Sprint(seen), "[2 3 1 3 2 1]"; got != want {
		t.Errorf("n n n N N N visited %s, want %s", got, want)
	}
	if header := ansi.Strip(p.renderHeader()); !strings.Contains(header, "1/3") || !strings.Contains(header, "alpha") {
		t.Errorf("header should show the query and counter, got %q", header)
	}
}

func TestPreviewSearchHeaderStaysOneLine(t *testing.T) {
	for _, width := range []int{20, 40, 120} {
		p := NewPreview(width, 12)
		p.SetSize(width, 12)
		p.SetSession(&session.Session{ID: "s", ProjectName: "proj", Status: session.StatusIdle})
		p.SetContent(numbered(40))
		searchFor(&p, strings.Repeat("long query ", 6))
		for _, typing := range []bool{true, false} {
			if !typing {
				p.CommitSearch()
			}
			if h := lipgloss.Height(p.renderHeader()); h != 2 {
				t.Errorf("width %d typing=%v: header is %d rows, want 2", width, typing, h)
			}
			if h := lipgloss.Height(p.View()); h > 12 {
				t.Errorf("width %d typing=%v: preview is %d rows, exceeds 12", width, typing, h)
			}
			for _, l := range strings.Split(p.View(), "\n") {
				if w := lipgloss.Width(l); w > width {
					t.Errorf("width %d typing=%v: a line is %d wide", width, typing, w)
				}
			}
		}
	}
}

func TestPreviewSearchScrollsToMatchAndClearRestores(t *testing.T) {
	p := searchPreview(numbered(60)) // following the bottom: lines 50-59 show
	if p.viewport.YOffset != 50 {
		t.Fatalf("precondition: YOffset = %d, want 50", p.viewport.YOffset)
	}
	searchFor(p, "line 07")
	if y := p.viewport.YOffset; y > 7 || y+p.viewport.Height <= 7 {
		t.Fatalf("YOffset = %d: line 7 is not in view", y)
	}
	// The match must survive the next capture instead of snapping to the bottom.
	p.SetContent(numbered(60) + "\nfresh output")
	if y := p.viewport.YOffset; y > 7 || y+p.viewport.Height <= 7 {
		t.Errorf("a refresh moved the view to YOffset %d, off the match", y)
	}
	if cur, total := p.SearchCount(); cur != 1 || total != 1 {
		t.Errorf("count after refresh = %d/%d, want 1/1", cur, total)
	}

	p.ClearSearch()
	if p.HasSearch() {
		t.Fatal("ClearSearch left the search on")
	}
	if !p.atBottom() || p.userScrolled {
		t.Errorf("clearing should return to following the bottom; YOffset = %d, userScrolled = %v", p.viewport.YOffset, p.userScrolled)
	}
	if strings.Contains(p.viewport.View(), "\x1b[") && strings.Contains(ansi.Strip(p.renderHeader()), "find:") {
		t.Error("clearing left search chrome behind")
	}
}

// While typing, matches are looked up from where the preview stood, wrapping to
// the top only when nothing lies below.
func TestPreviewSearchStartsFromTheViewedPosition(t *testing.T) {
	p := searchPreview(numbered(60))
	p.ScrollUp(30) // viewing lines 20-29
	searchFor(p, "line")
	if cur, total := p.SearchCount(); cur != 21 || total != 60 {
		t.Fatalf("count = %d/%d, want the first match in view, 21/60", cur, total)
	}
	p.ClearSearch()
	if y := p.viewport.YOffset; y != 20 || !p.userScrolled {
		t.Errorf("clearing should restore the scrolled position; YOffset = %d, userScrolled = %v", y, p.userScrolled)
	}
}

func TestPreviewSearchNoMatches(t *testing.T) {
	p := searchPreview(numbered(60))
	searchFor(p, "zzz")
	if cur, total := p.SearchCount(); cur != 0 || total != 0 {
		t.Errorf("count = %d/%d, want 0/0", cur, total)
	}
	if header := ansi.Strip(p.renderHeader()); !strings.Contains(header, "no matches") {
		t.Errorf("header = %q, want it to say no matches", header)
	}
	if !p.atBottom() {
		t.Errorf("a query without matches moved the view to YOffset %d", p.viewport.YOffset)
	}
	p.StepMatch(1) // must not panic or move
	// New content can bring matches to a kept query.
	p.CommitSearch()
	p.SetContent("zzz\n" + numbered(5))
	if _, total := p.SearchCount(); total != 1 {
		t.Errorf("total after matching content arrived = %d, want 1", total)
	}
}

func TestPreviewSearchEmptyQueryCommitClears(t *testing.T) {
	p := searchPreview(numbered(5))
	p.OpenSearch()
	if !p.HasSearch() || !p.SearchTyping() {
		t.Fatal("OpenSearch should open the input")
	}
	if p.CommitSearch() || p.HasSearch() {
		t.Error("Enter on an empty query should end the search")
	}
}

// The painted viewport never grows wider than the pane, whatever the content.
func TestPreviewSearchPaintedLinesFitTheViewport(t *testing.T) {
	trueColor(t)
	content := strings.Join([]string{
		"\x1b[32m" + strings.Repeat("match ", 20) + "\x1b[0m",
		strings.Repeat("日本match語", 10),
		"plain match",
	}, "\n")
	p := searchPreview(content)
	searchFor(p, "match")
	if _, total := p.SearchCount(); total < 3 {
		t.Fatalf("total = %d, want matches on every line", total)
	}
	for i, l := range strings.Split(p.viewport.View(), "\n") {
		if w := ansi.StringWidth(l); w > p.viewport.Width {
			t.Errorf("line %d is %d wide, viewport is %d", i, w, p.viewport.Width)
		}
	}
	if !strings.Contains(p.viewport.View(), styles.PreviewMatchCurrent.Render("match")) {
		t.Error("the current match is not highlighted in the viewport")
	}
}

// A group summary is searchable like any other preview content.
func TestPreviewSearchCoversGroupSummary(t *testing.T) {
	p := NewPreview(60, 20)
	p.SetSize(60, 20)
	p.SetGroupSummary("work", []*session.Session{{ID: "s", ProjectName: "naviClaude", Status: session.StatusIdle}})
	searchFor(&p, "naviclaude")
	if _, total := p.SearchCount(); total != 1 {
		t.Errorf("total = %d, want the project name found in the summary", total)
	}
}

// A changed query or current match must repaint, not reuse the memoized frame.
func TestPreviewSearchRepaintsMemoizedView(t *testing.T) {
	trueColor(t)
	p := searchPreview("one two\ntwo three")
	before := p.View()
	searchFor(p, "two")
	first := p.View()
	if first == before {
		t.Fatal("opening a search did not change the view")
	}
	p.CommitSearch()
	p.StepMatch(1)
	if p.View() == first {
		t.Error("moving to the next match did not repaint")
	}
}
