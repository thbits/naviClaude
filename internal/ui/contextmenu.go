package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tomhalo/naviclaude/internal/session"
	"github.com/tomhalo/naviclaude/internal/styles"
)

// MenuItem is a single entry in the context menu.
type MenuItem struct {
	Label    string
	Shortcut string
	Action   string
	Danger   bool
	IsSep    bool
}

// ContextMenuModel is a floating context menu for session actions.
type ContextMenuModel struct {
	visible bool
	x, y    int
	cursor  int
	items   []MenuItem
	session *session.Session
}

// NewContextMenu creates a ContextMenuModel.
func NewContextMenu() ContextMenuModel {
	return ContextMenuModel{}
}

// Show positions the menu and builds items based on the session status.
func (m *ContextMenuModel) Show(x, y int, s *session.Session) {
	m.visible = true
	m.x = x
	m.y = y
	m.session = s
	m.cursor = 0
	m.items = m.buildItems(s)
	m.skipSeparators(1) // ensure cursor is on a real item
}

// Hide closes the context menu.
func (m *ContextMenuModel) Hide() {
	m.visible = false
	m.cursor = 0
}

// IsVisible returns whether the menu is visible.
func (m *ContextMenuModel) IsVisible() bool {
	return m.visible
}

// SelectedAction returns the action identifier of the currently selected item.
func (m *ContextMenuModel) SelectedAction() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	item := m.items[m.cursor]
	if item.IsSep {
		return ""
	}
	return item.Action
}

// Session returns the session this context menu was opened for.
func (m *ContextMenuModel) Session() *session.Session {
	return m.session
}

// Init satisfies the tea.Model interface.
func (m ContextMenuModel) Init() tea.Cmd {
	return nil
}

// Update handles navigation and selection within the menu.
func (m ContextMenuModel) Update(msg tea.Msg) (ContextMenuModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			m.moveCursor(1)
		case "k", "up":
			m.moveCursor(-1)
		case "enter":
			// The main app reads SelectedAction().
			return m, nil
		case "esc":
			m.visible = false
		}
	}
	return m, nil
}

func (m *ContextMenuModel) moveCursor(dir int) {
	for {
		m.cursor += dir
		if m.cursor < 0 {
			m.cursor = 0
			return
		}
		if m.cursor >= len(m.items) {
			m.cursor = len(m.items) - 1
			return
		}
		if !m.items[m.cursor].IsSep {
			return
		}
	}
}

// skipSeparators ensures the cursor lands on a non-separator item.
func (m *ContextMenuModel) skipSeparators(dir int) {
	if len(m.items) == 0 {
		return
	}
	for m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].IsSep {
		m.cursor += dir
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
}

func (m *ContextMenuModel) buildItems(s *session.Session) []MenuItem {
	if s == nil {
		return nil
	}

	if s.Status == session.StatusClosed {
		return []MenuItem{
			{Label: "Resume", Shortcut: "Enter", Action: "resume"},
			{Label: "Fork & Resume", Shortcut: "", Action: "fork_resume"},
			{IsSep: true},
			{Label: "Detail", Shortcut: "d", Action: "detail"},
			{IsSep: true},
			{Label: "Copy session ID", Action: "copy_id"},
			{Label: "Copy project path", Action: "copy_path"},
		}
	}

	return []MenuItem{
		{Label: "Focus", Shortcut: "Enter", Action: "focus"},
		{Label: "Jump to pane", Shortcut: "f", Action: "jump"},
		{IsSep: true},
		{Label: "Detail", Shortcut: "d", Action: "detail"},
		{IsSep: true},
		{Label: "Copy session ID", Action: "copy_id"},
		{Label: "Copy project path", Action: "copy_path"},
		{IsSep: true},
		{Label: "Kill session", Shortcut: "K", Action: "kill", Danger: true},
	}
}

// View renders the context menu at its x,y position.
func (m ContextMenuModel) View() string {
	if !m.visible || len(m.items) == 0 {
		return ""
	}

	var rows []string
	maxWidth := 0

	for _, item := range m.items {
		w := len(item.Label)
		if item.Shortcut != "" {
			w += len(item.Shortcut) + 4 // space + parens
		}
		if w > maxWidth {
			maxWidth = w
		}
	}

	for i, item := range m.items {
		if item.IsSep {
			sep := styles.ContextMenuSep.Render(strings.Repeat("\u2500", maxWidth+2))
			rows = append(rows, sep)
			continue
		}

		isCursor := i == m.cursor
		label := item.Label

		if item.Shortcut != "" {
			gap := maxWidth - len(item.Label) - len(item.Shortcut) - 2
			if gap < 1 {
				gap = 1
			}
			shortcut := styles.ContextMenuShortcut.Render("(" + item.Shortcut + ")")
			label = item.Label + strings.Repeat(" ", gap) + shortcut
		} else {
			gap := maxWidth - len(item.Label)
			if gap < 0 {
				gap = 0
			}
			label = item.Label + strings.Repeat(" ", gap)
		}

		var row string
		switch {
		case item.Danger && isCursor:
			row = styles.ContextMenuItemDangerSelected.Render(label)
		case item.Danger:
			row = styles.ContextMenuItemDanger.Render(label)
		case isCursor:
			row = styles.ContextMenuItemSelected.Render(label)
		default:
			row = styles.ContextMenuItem.Render(label)
		}

		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	menu := styles.ContextMenuBorder.Render(content)

	// Position the menu at x,y using lipgloss.
	return lipgloss.NewStyle().
		MarginLeft(m.x).
		MarginTop(m.y).
		Render(menu)
}
