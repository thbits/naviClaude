package ui

import (
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
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
	visible      bool
	x, y         int
	cursor       int
	items        []MenuItem
	session      *session.Session
	termW, termH int
}

// NewContextMenu creates a ContextMenuModel.
func NewContextMenu() ContextMenuModel {
	return ContextMenuModel{}
}

// Show positions the menu and builds items based on the session status.
func (m *ContextMenuModel) Show(x, y int, s *session.Session, caps MenuCaps) {
	m.visible = true
	m.x = x
	m.y = y
	m.session = s
	m.cursor = 0
	m.items = m.buildItems(s, caps)
	m.skipSeparators(1) // ensure cursor is on a real item
}

// SetTermSize updates the terminal dimensions used for overflow clamping.
func (m *ContextMenuModel) SetTermSize(w, h int) {
	m.termW = w
	m.termH = h
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

// MenuCaps is what the session's harness supports, plus facts the menu can't look up itself.
type MenuCaps struct {
	CanResume bool
	CanFork   bool
	Branch    string // git branch to offer for copying; "" hides the item
}

func (m *ContextMenuModel) buildItems(s *session.Session, caps MenuCaps) []MenuItem {
	if s == nil {
		return nil
	}

	var items []MenuItem
	if s.Status == session.StatusClosed {
		if caps.CanResume {
			items = append(items,
				MenuItem{Label: "Resume", Shortcut: "Enter", Action: "resume"},
				MenuItem{Label: "Resume & jump", Shortcut: "f", Action: "resume_jump"},
			)
		}
		if caps.CanFork {
			items = append(items, MenuItem{Label: "Fork & Resume", Shortcut: "", Action: "fork_resume"})
		}
		if len(items) > 0 {
			items = append(items, MenuItem{IsSep: true})
		}
		items = append(items, MenuItem{Label: "Detail", Shortcut: "d", Action: "detail"})
	} else {
		items = append(items,
			MenuItem{Label: "Focus", Shortcut: "Enter", Action: "focus"},
			MenuItem{Label: "Jump to pane", Shortcut: "f", Action: "jump"},
			MenuItem{IsSep: true},
			MenuItem{Label: "Detail", Shortcut: "d", Action: "detail"},
		)
		// Only an idle session can show the done mark, so only it can be re-marked.
		if s.Status == session.StatusIdle && s.Attention == "" && s.TmuxPaneID != "" {
			items = append(items, MenuItem{Label: "Set as unread", Action: "mark_unread"})
		}
	}

	if s.CWD != "" {
		items = append(items,
			MenuItem{IsSep: true},
			MenuItem{Label: "Open in editor", Action: "open_editor"},
			MenuItem{Label: fileManagerLabel(), Action: "open_folder"},
		)
	}

	items = append(items,
		MenuItem{IsSep: true},
		MenuItem{Label: "Copy session ID", Action: "copy_id"},
		MenuItem{Label: "Copy project path", Action: "copy_path"},
	)
	if caps.Branch != "" {
		items = append(items, MenuItem{Label: "Copy branch name", Action: "copy_branch"})
	}
	if s.Status == session.StatusClosed {
		if caps.CanResume {
			items = append(items, MenuItem{Label: "Copy resume command", Action: "copy_resume"})
		}
		return items
	}
	return append(items,
		MenuItem{IsSep: true},
		MenuItem{Label: "Kill session", Shortcut: "K", Action: "kill", Danger: true},
	)
}

func fileManagerLabel() string {
	if runtime.GOOS == "darwin" {
		return "Open in Finder"
	}
	return "Open in file manager"
}

// renderBox renders the bordered menu without positioning margins.
func (m ContextMenuModel) renderBox() string {
	var rows []string
	maxWidth := 0

	// Measure display columns, not bytes, to match Rect's placement clamp and
	// stay correct for multibyte labels.
	for _, item := range m.items {
		w := lipgloss.Width(item.Label)
		if item.Shortcut != "" {
			w += lipgloss.Width(item.Shortcut) + 4 // space + parens
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
		var label string

		if item.Shortcut != "" {
			gap := maxWidth - lipgloss.Width(item.Label) - lipgloss.Width(item.Shortcut) - 2
			if gap < 1 {
				gap = 1
			}
			shortcut := styles.ContextMenuShortcut.Render("(" + item.Shortcut + ")")
			label = item.Label + strings.Repeat(" ", gap) + shortcut
		} else {
			// No shortcut: left-pad the label out to the column width (shared
			// display-width-aware helper, floors the pad at 0).
			label = item.Label + strings.Repeat(" ", alignedLabelPad(item.Label, maxWidth, 0))
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

	return styles.ContextMenuBorder.Render(strings.Join(rows, "\n"))
}

// Rect returns the menu's on-screen rectangle after clamping it inside the
// terminal; all zero when the menu is hidden or empty.
func (m ContextMenuModel) Rect() (x, y, w, h int) {
	if !m.visible || len(m.items) == 0 {
		return 0, 0, 0, 0
	}
	return m.place(m.renderBox())
}

func (m ContextMenuModel) place(menu string) (x, y, w, h int) {
	w = lipgloss.Width(menu)
	h = lipgloss.Height(menu)
	x, y = m.x, m.y
	if m.termW > 0 && x+w > m.termW {
		x = m.termW - w
	}
	if x < 0 {
		x = 0
	}
	if m.termH > 0 && y+h > m.termH {
		y = m.termH - h
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}

// Contains reports whether the screen cell (px, py) is inside the menu box.
func (m ContextMenuModel) Contains(px, py int) bool {
	x, y, w, h := m.Rect()
	return InRect(px, py, x, y, w, h)
}

// ItemAt returns the index of the selectable item at screen cell (px, py).
// Borders and separators are not selectable.
func (m ContextMenuModel) ItemAt(px, py int) (int, bool) {
	x, y, w, h := m.Rect()
	if !InRect(px, py, x+1, y+1, w-2, h-2) {
		return 0, false
	}
	idx := py - y - 1
	if idx < 0 || idx >= len(m.items) || m.items[idx].IsSep {
		return 0, false
	}
	return idx, true
}

// SetCursor highlights item idx when it is a selectable row.
func (m *ContextMenuModel) SetCursor(idx int) {
	if idx >= 0 && idx < len(m.items) && !m.items[idx].IsSep {
		m.cursor = idx
	}
}

// View renders the bare menu box; Rect gives where it belongs on screen.
func (m ContextMenuModel) View() string {
	if !m.visible || len(m.items) == 0 {
		return ""
	}
	return m.renderBox()
}
