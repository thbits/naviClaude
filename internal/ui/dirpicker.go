package ui

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/junegunn/fzf/src/util"
	"github.com/thbits/naviClaude/internal/styles"
)

// DirCandidate is one selectable directory in the picker.
type DirCandidate struct {
	Path   string // absolute, cleaned path
	Source string // base | subdir | zoxide | session | walk
}

// DirPickerModel is an overlay for choosing a new session's directory: typing
// fzf-filters the tree under base and $HOME plus zoxide and session dirs, while
// ->/<- walk the tree.
type DirPickerModel struct {
	input       textinput.Model
	base        string
	sessionDirs []string
	subdirs     []string // immediate children of base (fast first stage)
	zoxideDirs  []string
	walkedDirs  []string // breadth-first walk of base, per base
	homeDirs    []string // breadth-first walk of $HOME, once per Show
	walkingBase bool
	walkingHome bool
	showID      int // tags the home walk so a previous Show's result is dropped
	candidates  []DirCandidate
	display     []string     // ~-shortened candidate paths, what is matched and drawn
	chars       []util.Chars // display, pre-encoded for fzf
	filtered    []DirCandidate
	terms       []fzfTerm // parsed query behind filtered, reused for highlighting
	query       string    // query behind filtered, to skip no-op refilters
	matcher     *fzfMatcher
	cursor      int
	visible     bool
	width       int
	height      int
	title       string
	homeDir     string // cached on Show so displayPath doesn't stat $HOME per row/frame

	zoxideFn   func() []string       // frecent dirs; default queries zoxide
	listDirsFn func(string) []string // subdirs of a base; default reads the FS
	walkFn     func(root string, depth int) []string
}

// NewDirPicker creates a DirPickerModel wired to the real zoxide and filesystem.
func NewDirPicker() DirPickerModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = styles.SearchPrompt
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)
	ti.Placeholder = "filter or navigate (->/<-) ..."
	ti.CharLimit = 256
	return DirPickerModel{
		input:      ti,
		title:      "New session directory",
		zoxideFn:   defaultZoxideDirs,
		listDirsFn: listSubdirs,
		walkFn:     defaultWalkDirs,
		matcher:    newFzfMatcher(),
	}
}

// dirCandidatesMsg carries the fast lookups for a base (subdir listing and
// zoxide query), run off the Update goroutine.
type dirCandidatesMsg struct {
	base    string
	subdirs []string
	zoxide  []string
}

// dirWalkMsg carries the recursive directory walk for a base.
type dirWalkMsg struct {
	base string
	dirs []string
}

// dirHomeWalkMsg carries the $HOME walk for the Show identified by showID.
type dirHomeWalkMsg struct {
	showID int
	dirs   []string
}

// Show opens the picker at base and returns a command that loads candidates off
// the Update goroutine; base is preselected so Enter keeps the caller's default.
func (m *DirPickerModel) Show(base string, sessionDirs []string) tea.Cmd {
	m.visible = true
	m.sessionDirs = sessionDirs
	m.input.SetValue("")
	m.input.Focus()
	if h, err := os.UserHomeDir(); err == nil {
		m.homeDir = h
	}
	m.showID++
	m.homeDirs = nil
	m.walkingHome = m.homeDir != ""
	return tea.Batch(m.setPendingBase(base), m.homeWalkCmd())
}

// SetTitle overrides the popup title (e.g. to reflect the n vs N flow).
func (m *DirPickerModel) SetTitle(title string) { m.title = title }

// Hide closes the picker and clears its state.
func (m *DirPickerModel) Hide() {
	m.visible = false
	m.input.SetValue("")
	m.input.Blur()
	m.subdirs, m.zoxideDirs, m.walkedDirs, m.homeDirs = nil, nil, nil, nil
	m.walkingBase, m.walkingHome = false, false
	m.candidates, m.display, m.chars = nil, nil, nil
	m.filtered, m.terms, m.query = nil, nil, ""
	m.cursor = 0
}

// IsVisible reports whether the picker is open.
func (m *DirPickerModel) IsVisible() bool { return m.visible }

// SetSize updates the popup container dimensions.
func (m *DirPickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if w > 8 {
		m.input.Width = w - 8
	}
}

// Selected returns the highlighted candidate's path, or "" when the list is
// empty.
func (m *DirPickerModel) Selected() string {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return ""
	}
	return m.filtered[m.cursor].Path
}

// MoveUp / MoveDown move the cursor within the filtered list.
func (m *DirPickerModel) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *DirPickerModel) MoveDown() {
	if m.cursor < len(m.filtered)-1 {
		m.cursor++
	}
}

// Descend makes the highlighted directory the new base and returns the command that
// loads its candidates, or nil when nothing is selected.
func (m *DirPickerModel) Descend() tea.Cmd {
	sel := m.Selected()
	if sel == "" {
		return nil
	}
	m.input.SetValue("")
	return m.setPendingBase(sel)
}

// Parent navigates to the parent of the current base and returns the command
// that loads its candidates. Returns nil at the filesystem root.
func (m *DirPickerModel) Parent() tea.Cmd {
	parent := filepath.Dir(m.base)
	if parent == "" || parent == m.base {
		return nil
	}
	m.input.SetValue("")
	return m.setPendingBase(parent)
}

// setPendingBase sets the new base and shows it (plus session dirs) at once, then
// returns commands for the blocking lookups so Update never waits on them.
func (m *DirPickerModel) setPendingBase(base string) tea.Cmd {
	m.base = absClean(base)
	m.subdirs, m.zoxideDirs, m.walkedDirs = nil, nil, nil
	m.walkingBase = true
	m.filtered = nil
	m.cursor = 0
	m.rebuild()
	return tea.Batch(m.loadCandidatesCmd(m.base), m.walkCmd(m.base))
}

// loadCandidatesCmd returns a command that runs the fast zoxide and subdir
// lookups for base.
func (m *DirPickerModel) loadCandidatesCmd(base string) tea.Cmd {
	zoxideFn := m.zoxideFn
	listDirsFn := m.listDirsFn
	return func() tea.Msg {
		return dirCandidatesMsg{base: base, subdirs: listDirsFn(base), zoxide: zoxideFn()}
	}
}

// walkCmd returns a command that walks the tree under base.
func (m *DirPickerModel) walkCmd(base string) tea.Cmd {
	walkFn := m.walkFn
	return func() tea.Msg {
		return dirWalkMsg{base: base, dirs: walkFn(base, walkBaseDepth)}
	}
}

// homeWalkCmd returns a command that walks $HOME, so dirs outside base are
// reachable without zoxide; it runs once per Show, not per navigation.
func (m *DirPickerModel) homeWalkCmd() tea.Cmd {
	if m.homeDir == "" {
		return nil
	}
	walkFn := m.walkFn
	home, id := m.homeDir, m.showID
	return func() tea.Msg {
		return dirHomeWalkMsg{showID: id, dirs: append([]string{home}, walkFn(home, walkHomeDepth)...)}
	}
}

// applyCandidates installs the fast lookups, ignoring results for a base the
// user has already navigated away from.
func (m *DirPickerModel) applyCandidates(msg dirCandidatesMsg) {
	if !m.visible || msg.base != m.base {
		return
	}
	m.subdirs, m.zoxideDirs = msg.subdirs, msg.zoxide
	m.rebuild()
}

// applyWalk installs the recursive walk, ignoring stale bases.
func (m *DirPickerModel) applyWalk(msg dirWalkMsg) {
	if !m.visible || msg.base != m.base {
		return
	}
	m.walkedDirs = msg.dirs
	m.walkingBase = false
	m.rebuild()
}

// applyHomeWalk installs the $HOME walk, ignoring a previous Show's result.
func (m *DirPickerModel) applyHomeWalk(msg dirHomeWalkMsg) {
	if !m.visible || msg.showID != m.showID {
		return
	}
	m.homeDirs = msg.dirs
	m.walkingHome = false
	m.rebuild()
}

// rebuild regenerates the candidate pool from every source loaded so far. With
// no query the highlighted path is kept; with one the cursor goes to the best match.
func (m *DirPickerModel) rebuild() {
	prev := ""
	if m.input.Value() == "" {
		prev = m.Selected()
	} else {
		m.cursor = 0
	}
	walked := append(append([]string(nil), m.walkedDirs...), m.homeDirs...)
	m.candidates = buildCandidates(m.base, m.subdirs, m.zoxideDirs, m.sessionDirs, walked)
	m.display = make([]string, len(m.candidates))
	m.chars = make([]util.Chars, len(m.candidates))
	for i, c := range m.candidates {
		m.display[i] = m.displayPath(c.Path)
		m.chars[i] = util.ToChars([]byte(m.display[i]))
	}
	m.refilter()
	if prev != "" {
		for i, c := range m.filtered {
			if c.Path == prev {
				m.cursor = i
				break
			}
		}
	}
}

// refilter re-runs the query over the pool, clamping the cursor.
func (m *DirPickerModel) refilter() {
	m.query = m.input.Value()
	m.terms = parseFzfQuery(m.query)
	if m.matcher == nil {
		m.matcher = newFzfMatcher()
	}
	m.filtered = filterCandidates(m.matcher, m.terms, m.candidates, m.chars)
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Init satisfies tea.Model.
func (m DirPickerModel) Init() tea.Cmd { return textinput.Blink }

// Update feeds typing to the input and refilters, and applies async candidate
// loads; navigation keys are handled by the app via the methods above.
func (m DirPickerModel) Update(msg tea.Msg) (DirPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case dirCandidatesMsg:
		m.applyCandidates(msg)
		return m, nil
	case dirWalkMsg:
		m.applyWalk(msg)
		return m, nil
	case dirHomeWalkMsg:
		m.applyHomeWalk(msg)
		return m, nil
	}
	if !m.visible {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Cursor blinks also land here; only a changed query is worth a refilter.
	if m.input.Value() != m.query {
		m.cursor = 0
		m.refilter()
	}
	return m, cmd
}

// ---------------------------------------------------------------------------
// Pure candidate logic (unit-tested directly)
// ---------------------------------------------------------------------------

// buildCandidates assembles the de-duplicated pool: base first (so Enter can pick
// it), then subdirectories, zoxide dirs, session dirs and the recursive walk.
func buildCandidates(base string, subdirs, zoxideDirs, sessionDirs, walkedDirs []string) []DirCandidate {
	var out []DirCandidate
	seen := make(map[string]bool)
	add := func(path, source string) {
		ap := absClean(path)
		if ap == "" || seen[ap] {
			return
		}
		seen[ap] = true
		out = append(out, DirCandidate{Path: ap, Source: source})
	}
	add(base, "base")
	for _, d := range subdirs {
		add(d, "subdir")
	}
	for _, d := range zoxideDirs {
		add(d, "zoxide")
	}
	for _, d := range sessionDirs {
		add(d, "session")
	}
	for _, d := range walkedDirs {
		add(d, "walk")
	}
	return out
}

// filterCandidates ranks candidates against the parsed query with fzf's
// algorithm over chars (their display paths). No terms keeps the pool order.
func filterCandidates(fm *fzfMatcher, terms []fzfTerm, candidates []DirCandidate, chars []util.Chars) []DirCandidate {
	if len(terms) == 0 {
		return candidates
	}
	matches := fm.match(terms, chars)
	out := make([]DirCandidate, len(matches))
	for i, mt := range matches {
		out[i] = candidates[mt.index]
	}
	return out
}

// absClean expands a leading ~ and returns an absolute, cleaned path.
func absClean(p string) string {
	if p == "" {
		return ""
	}
	p = expandTilde(p)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// expandTilde expands a leading ~ or ~/ to the user's home directory.
func expandTilde(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// listSubdirs returns the absolute paths of base's immediate, non-hidden
// subdirectories (symlinks to directories included), sorted. Returns nil when
// base can't be read.
func listSubdirs(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		p := filepath.Join(base, e.Name())
		if strings.HasPrefix(e.Name(), ".") || !isDirEntry(p, e) {
			continue
		}
		dirs = append(dirs, p)
	}
	sort.Strings(dirs)
	return dirs
}

// isDirEntry reports whether e is a directory, following a symlink to one.
func isDirEntry(p string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// Walk bounds: deep under base, shallower across $HOME, capped in total.
const (
	walkBaseDepth = 6
	walkHomeDepth = 4
	walkMaxDirs   = 50000
)

// walkSkipNames are dependency/cache trees nobody starts a session in.
var walkSkipNames = map[string]bool{
	"node_modules":  true,
	"__pycache__":   true,
	"site-packages": true,
}

// walkRoot is a directory to walk and how many levels below it to descend.
type walkRoot struct {
	dir   string
	depth int
}

// defaultWalkDirs walks root depth levels deep, skipping junk trees and the
// macOS ~/Library.
func defaultWalkDirs(root string, depth int) []string {
	library := ""
	if h, err := os.UserHomeDir(); err == nil {
		library = filepath.Join(h, "Library")
	}
	skip := func(p, name string) bool { return walkSkipNames[name] || p == library }
	return walkDirs([]walkRoot{{root, depth}}, walkMaxDirs, skip)
}

// walkDirs lists non-hidden directories under roots breadth-first, so the cap
// drops the deepest levels rather than whole late-alphabet subtrees. Unreadable
// dirs are skipped and symlinked dirs are listed but not descended into.
func walkDirs(roots []walkRoot, limit int, skip func(p, name string) bool) []string {
	seen := make(map[string]bool)
	var queue []walkRoot
	for _, r := range roots {
		if r.dir != "" && !seen[r.dir] {
			seen[r.dir] = true
			queue = append(queue, r)
		}
	}
	var out []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth <= 0 {
			continue
		}
		entries, err := os.ReadDir(cur.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			p := filepath.Join(cur.dir, name)
			if strings.HasPrefix(name, ".") || seen[p] || skip(p, name) || !isDirEntry(p, e) {
				continue
			}
			seen[p] = true
			out = append(out, p)
			if len(out) >= limit {
				return out
			}
			if e.IsDir() {
				queue = append(queue, walkRoot{p, cur.depth - 1})
			}
		}
	}
	return out
}

// defaultZoxideDirs returns the user's zoxide frecent directories, most frecent
// first. Returns nil when zoxide is not installed or fails.
func defaultZoxideDirs() []string {
	out, err := exec.Command("zoxide", "query", "-l").Output()
	if err != nil {
		return nil
	}
	var dirs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dirs = append(dirs, line)
		}
	}
	return dirs
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// displayPath shortens a home-prefixed path to ~, using the home dir cached on
// Show and falling back to a live lookup if it is unset.
func (m DirPickerModel) displayPath(p string) string {
	h := m.homeDir
	if h == "" {
		h, _ = os.UserHomeDir()
	}
	if h != "" {
		if p == h {
			return "~"
		}
		if strings.HasPrefix(p, h+string(filepath.Separator)) {
			return "~" + p[len(h):]
		}
	}
	return p
}

// sourceMarker is a one-char gutter tag for a candidate's origin (ASCII only).
func sourceMarker(source string) string {
	switch source {
	case "base":
		return "."
	case "zoxide":
		return "*"
	case "session":
		return "@"
	default: // subdir, walk
		return "/"
	}
}

// highlightMatches renders text with the runes at pos drawn in hl, the rest in
// plain, like fzf's match highlighting.
func highlightMatches(text string, pos map[int]bool, plain, hl lipgloss.Style) string {
	if len(pos) == 0 {
		return plain.Render(text)
	}
	var b strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && pos[j] == pos[i] {
			j++
		}
		style := plain
		if pos[i] {
			style = hl
		}
		b.WriteString(style.Render(string(runes[i:j])))
		i = j
	}
	return b.String()
}

// dirPickerChromeRows is the popup rows taken by non-candidate chrome: border (2),
// padding (2), title, base line, input, two separators (2), footer.
const dirPickerChromeRows = 10

// visibleRows returns how many candidate rows fit (3-20) after dirPickerChromeRows.
func (m DirPickerModel) visibleRows() int {
	rows := m.height - dirPickerChromeRows
	if rows < 3 {
		rows = 3
	}
	if rows > 20 {
		rows = 20
	}
	return rows
}

// window returns the [start, end) slice of m.filtered shown around the cursor.
func (m DirPickerModel) window() (int, int) {
	max := m.visibleRows()
	start := 0
	if m.cursor >= max {
		start = m.cursor - max + 1
	}
	end := start + max
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	return start, end
}

// dirPickerFirstRow is the popup-local row of the first candidate: border (1)
// + padding (1) + title, base line, input, separator (4).
const dirPickerFirstRow = 6

// RowAt returns the candidate index drawn at popup-local row y.
func (m DirPickerModel) RowAt(y int) (int, bool) {
	start, end := m.window()
	i := start + y - dirPickerFirstRow
	if y < dirPickerFirstRow || i >= end {
		return 0, false
	}
	return i, true
}

// Cursor returns the highlighted candidate index.
func (m DirPickerModel) Cursor() int { return m.cursor }

// SetCursor highlights candidate i.
func (m *DirPickerModel) SetCursor(i int) {
	if i >= 0 && i < len(m.filtered) {
		m.cursor = i
	}
}

// View renders the picker popup (placement is done by the caller via PlaceOverlay).
func (m DirPickerModel) View() string {
	if !m.visible {
		return ""
	}

	boxWidth := m.width / 2
	if boxWidth < 40 {
		boxWidth = 40
	}

	title := lipgloss.NewStyle().Foreground(styles.ColorPurple).Bold(true).Render(m.title)
	// fzf-style "matched/total" counter, with a marker while the walk runs.
	count := fmt.Sprintf("  %d/%d", len(m.filtered), len(m.candidates))
	if m.walkingBase || m.walkingHome {
		count += " …"
	}
	baseLine := lipgloss.NewStyle().Foreground(styles.ColorGray).
		Render("base: " + m.displayPath(m.base) + count)
	sep := lipgloss.NewStyle().Foreground(styles.ColorGray).
		Render(strings.Repeat("─", boxWidth))

	var rows []string
	rows = append(rows, title)
	rows = append(rows, baseLine)
	rows = append(rows, m.input.View())
	rows = append(rows, sep)

	if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(styles.ColorGray).Render("  (no matching directories)"))
	} else {
		start, end := m.window()
		for i := start; i < end; i++ {
			c := m.filtered[i]
			text := m.displayPath(c.Path)
			var pos map[int]bool
			if len(m.terms) > 0 {
				pos = matchPositions(m.terms, text)
			}
			row := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
			plain := lipgloss.NewStyle().Foreground(styles.ColorFg)
			hl := lipgloss.NewStyle().Foreground(styles.ColorGreen).Bold(true)
			marker := lipgloss.NewStyle().Foreground(styles.ColorAmber)
			if i == m.cursor {
				row = row.Background(styles.ColorSelection)
				plain = plain.Foreground(styles.ColorBlue).Background(styles.ColorSelection).Bold(true)
				hl = hl.Background(styles.ColorSelection)
				marker = marker.Background(styles.ColorSelection)
			}
			sp := lipgloss.NewStyle()
			if i == m.cursor {
				sp = sp.Background(styles.ColorSelection)
			}
			label := marker.Render(sourceMarker(c.Source)) + sp.Render(" ") + highlightMatches(text, pos, plain, hl)
			rows = append(rows, row.Render(label))
		}
	}

	rows = append(rows, sep)
	footer := lipgloss.NewStyle().Foreground(styles.ColorGray).
		Render("type filter  ↑↓ move  → enter dir  ← parent  Enter select  Esc cancel")
	rows = append(rows, footer)

	content := strings.Join(rows, "\n")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorPurple).
		Background(styles.ColorBgPanel).
		Padding(1, 2)
	return border.Render(content)
}
