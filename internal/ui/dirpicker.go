package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/thbits/naviClaude/internal/styles"
)

// DirCandidate is one selectable directory in the picker.
type DirCandidate struct {
	Path   string // absolute, cleaned path
	Source string // base | subdir | zoxide | session
}

// DirPickerModel is an overlay for choosing a new session's directory: typing
// fuzzy-filters subdirs, zoxide and session dirs, while ->/<- walk the tree.
type DirPickerModel struct {
	input       textinput.Model
	base        string
	sessionDirs []string
	candidates  []DirCandidate
	filtered    []DirCandidate
	cursor      int
	visible     bool
	width       int
	height      int
	title       string
	homeDir     string // cached on Show so displayPath doesn't stat $HOME per row/frame

	zoxideFn   func() []string       // frecent dirs; default queries zoxide
	listDirsFn func(string) []string // subdirs of a base; default reads the FS
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
	}
}

// dirCandidatesMsg carries the result of an async candidate lookup for a base
// directory (the zoxide query + subdir listing run off the Update goroutine).
type dirCandidatesMsg struct {
	base       string
	candidates []DirCandidate
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
	return m.setPendingBase(base)
}

// SetTitle overrides the popup title (e.g. to reflect the n vs N flow).
func (m *DirPickerModel) SetTitle(title string) { m.title = title }

// Hide closes the picker and clears its state.
func (m *DirPickerModel) Hide() {
	m.visible = false
	m.input.SetValue("")
	m.input.Blur()
	m.candidates = nil
	m.filtered = nil
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

// setPendingBase sets the new base and clears stale candidates, returning a command
// for the blocking lookups so the Update goroutine never waits on them.
func (m *DirPickerModel) setPendingBase(base string) tea.Cmd {
	m.base = absClean(base)
	m.candidates = nil
	m.filtered = nil
	m.cursor = 0
	return m.loadCandidatesCmd(m.base)
}

// loadCandidatesCmd returns a command that runs the blocking zoxide and subdir
// lookups for base and emits the built candidate pool.
func (m *DirPickerModel) loadCandidatesCmd(base string) tea.Cmd {
	zoxideFn := m.zoxideFn
	listDirsFn := m.listDirsFn
	sessionDirs := m.sessionDirs
	return func() tea.Msg {
		cands := buildCandidates(base, listDirsFn(base), zoxideFn(), sessionDirs)
		return dirCandidatesMsg{base: base, candidates: cands}
	}
}

// applyCandidates installs loaded candidates and refilters, ignoring results for a
// base the user has already navigated away from.
func (m *DirPickerModel) applyCandidates(msg dirCandidatesMsg) {
	if !m.visible || msg.base != m.base {
		return
	}
	m.candidates = msg.candidates
	m.runFilter()
}

func (m *DirPickerModel) runFilter() {
	m.filtered = filterCandidates(m.input.Value(), m.candidates)
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
	if msg, ok := msg.(dirCandidatesMsg); ok {
		m.applyCandidates(msg)
		return m, nil
	}
	if !m.visible {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.runFilter()
	return m, cmd
}

// ---------------------------------------------------------------------------
// Pure candidate logic (unit-tested directly)
// ---------------------------------------------------------------------------

// buildCandidates assembles the de-duplicated pool: base first (so Enter can pick
// it), then subdirectories, zoxide dirs, and open-session dirs.
func buildCandidates(base string, subdirs, zoxideDirs, sessionDirs []string) []DirCandidate {
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
	return out
}

// candSource adapts candidates for sahilm/fuzzy, matching on the full path.
type candSource []DirCandidate

func (c candSource) String(i int) string { return c[i].Path }
func (c candSource) Len() int            { return len(c) }

// filterCandidates fuzzy-filters by query (matching the path). An empty query
// returns the candidates unchanged (preserving the base-first order).
func filterCandidates(query string, candidates []DirCandidate) []DirCandidate {
	if strings.TrimSpace(query) == "" {
		return candidates
	}
	matches := fuzzy.FindFrom(query, candSource(candidates))
	out := make([]DirCandidate, len(matches))
	for i, mt := range matches {
		out[i] = candidates[mt.Index]
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
// subdirectories, sorted. Returns nil when base can't be read.
func listSubdirs(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, filepath.Join(base, e.Name()))
	}
	sort.Strings(dirs)
	return dirs
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
	default: // subdir
		return "/"
	}
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
	baseLine := lipgloss.NewStyle().Foreground(styles.ColorGray).
		Render("base: " + m.displayPath(m.base))
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
			marker := lipgloss.NewStyle().Foreground(styles.ColorAmber).Render(sourceMarker(c.Source))
			label := marker + " " + m.displayPath(c.Path)
			if i == m.cursor {
				rows = append(rows, lipgloss.NewStyle().
					Foreground(styles.ColorBlue).
					Background(styles.ColorSelection).
					Bold(true).
					PaddingLeft(1).PaddingRight(1).
					Render(label))
			} else {
				rows = append(rows, lipgloss.NewStyle().
					Foreground(styles.ColorFg).
					PaddingLeft(1).PaddingRight(1).
					Render(label))
			}
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
