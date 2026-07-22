package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// ChangedFilesModel is the right-hand panel listing the files the selected
// session edited, each with its accumulated +added/-removed counts. It mirrors
// the SidebarModel component shape: SetSize / SetFiles / Update / View, with a
// cursor navigable by j/k and a SelectedFile accessor for opening in $EDITOR.
type ChangedFilesModel struct {
	files  []session.ChangedFile
	cwd    string // session working directory, used to show relative paths
	cursor int
	width  int
	height int
	vp     viewport.Model
}

// NewChangedFiles creates a ChangedFilesModel with the given dimensions.
func NewChangedFiles(width, height int) ChangedFilesModel {
	vp := viewport.New(width, height-1) // -1 for header
	return ChangedFilesModel{
		width:  width,
		height: height,
		vp:     vp,
	}
}

// SetSize updates the panel dimensions.
func (m *ChangedFilesModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	listHeight := h - 1 // header takes 1 line
	if listHeight < 1 {
		listHeight = 1
	}
	m.vp.Width = w
	m.vp.Height = listHeight
	m.syncViewport()
}

// SetFiles replaces the file list and records the session CWD used to show
// relative paths. The cursor resets to the top because the list belongs to a
// (possibly) different session.
func (m *ChangedFilesModel) SetFiles(files []session.ChangedFile, cwd string) {
	m.files = files
	m.cwd = cwd
	m.cursor = 0
	m.syncViewport()
}

// Reset clears the file list (e.g. when the selection moves to a group header).
func (m *ChangedFilesModel) Reset() {
	m.files = nil
	m.cwd = ""
	m.cursor = 0
	m.syncViewport()
}

// Count returns the number of changed files.
func (m *ChangedFilesModel) Count() int { return len(m.files) }

// SelectedFile returns the absolute path of the file under the cursor, or the
// empty string when there is no selection.
func (m *ChangedFilesModel) SelectedFile() string {
	if m.cursor < 0 || m.cursor >= len(m.files) {
		return ""
	}
	return m.files[m.cursor].Path
}

// Init satisfies the tea.Model interface.
func (m ChangedFilesModel) Init() tea.Cmd { return nil }

// Update handles navigation keys (j/k/g/G). Opening the selected file and
// closing the panel are handled by the app, which owns $EDITOR and mode state.
func (m ChangedFilesModel) Update(msg tea.Msg) (ChangedFilesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.files)-1 {
				m.cursor++
			}
			m.syncViewport()
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.syncViewport()
		case "G":
			if len(m.files) > 0 {
				m.cursor = len(m.files) - 1
			}
			m.syncViewport()
		case "g":
			m.cursor = 0
			m.syncViewport()
		}
	}
	return m, nil
}

// syncViewport re-renders the rows and scrolls so the cursor row is visible.
// Each file is exactly one row, so the cursor's line index equals its index.
func (m *ChangedFilesModel) syncViewport() {
	rows := make([]string, len(m.files))
	for i, f := range m.files {
		rows[i] = m.renderRow(f, i == m.cursor)
	}
	m.vp.SetContent(strings.Join(rows, "\n"))

	yOff := m.vp.YOffset
	if m.cursor < yOff {
		yOff = m.cursor
	}
	if m.cursor >= yOff+m.vp.Height {
		yOff = m.cursor - m.vp.Height + 1
	}
	if yOff < 0 {
		yOff = 0
	}
	m.vp.SetYOffset(yOff)
}

// renderRow renders one file row: the (relative) path on the left and the
// +added/-removed counts on the right, filling the gap between.
func (m ChangedFilesModel) renderRow(f session.ChangedFile, selected bool) string {
	stats := m.renderStats(f)
	statsWidth := lipgloss.Width(stats)

	// -1 leaves a trailing space; the selection style adds a 1-col left border,
	// so both selected and unselected rows reserve one leading column.
	nameWidth := m.width - statsWidth - 2
	if nameWidth < 1 {
		nameWidth = 1
	}
	name := truncateDisplay(m.relPath(f.Path), nameWidth)

	gap := m.width - lipgloss.Width(name) - statsWidth - 2
	if gap < 1 {
		gap = 1
	}
	content := name + strings.Repeat(" ", gap) + stats

	if selected {
		return styles.SidebarItemSelected.Render(content)
	}
	return styles.SidebarItem.Render(content)
}

// renderStats renders "+A" in green and "-R" in red, omitting a zero side.
// Estimated counts (no live git diff, e.g. already committed) render faintly.
// Styles are built per render from the active theme's colors so they follow
// theme switches (the package-level color vars are reassigned by ApplyTheme).
func (m ChangedFilesModel) renderStats(f session.ChangedFile) string {
	added := lipgloss.NewStyle().Foreground(styles.ColorGreen)
	removed := lipgloss.NewStyle().Foreground(styles.ColorRed)
	if f.Estimated {
		added = added.Faint(true)
		removed = removed.Faint(true)
	}
	var parts []string
	if f.Added > 0 {
		parts = append(parts, added.Render(fmt.Sprintf("+%d", f.Added)))
	}
	if f.Removed > 0 {
		parts = append(parts, removed.Render(fmt.Sprintf("-%d", f.Removed)))
	}
	return strings.Join(parts, " ")
}

// relPath renders path relative to the session CWD when possible, falling back
// to the base name so rows stay readable in a narrow panel.
func (m ChangedFilesModel) relPath(path string) string {
	if m.cwd != "" {
		if rel, err := filepath.Rel(m.cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(path)
}

// View renders the panel: a header plus the scrollable file list (or an empty
// state when the session edited nothing).
func (m ChangedFilesModel) View() string {
	title := styles.SidebarTitle.Render("CHANGED FILES")
	countStr := styles.SidebarTitleCount.Render(fmt.Sprintf("%d files", len(m.files)))
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(countStr) - 2
	if gap < 1 {
		gap = 1
	}
	header := title + strings.Repeat(" ", gap) + countStr + " "

	if len(m.files) == 0 {
		body := lipgloss.Place(m.width, m.vp.Height, lipgloss.Center, lipgloss.Center,
			styles.EmptyState.Render("No files changed"))
		return lipgloss.JoinVertical(lipgloss.Left, header, body)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, m.vp.View())
}
