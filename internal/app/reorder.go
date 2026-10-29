package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// tmuxMover moves windows and renames sessions; tests stub it.
type tmuxMover interface {
	MoveWindow(srcPane, dstPane string, before bool) error
	MoveWindowToSession(srcPane, name string) error
	RenameSession(oldName, newName string) error
}

// rowDrag is a left press on a sidebar row or group header that turns into a
// drag once the pointer reaches another item.
type rowDrag struct {
	pane   string // dragged session's tmux pane; "" for a group drag
	group  string // dragged group, for a group drag
	key    string // the pressed item, as selectionIdentity names it
	active bool   // the pointer left the pressed item
	ok     bool   // the pointer is over a valid drop
	sess   ui.SessionDrop
	grp    ui.GroupDrop
}

// windowMovedMsg reports a move-window or rename-session, with naviClaude's own
// tmux location re-read since either can change it.
type windowMovedMsg struct {
	err             error
	renamedFrom     string // old group name after a rename
	renamedTo       string
	curSess, curWin string
}

func (m Model) tmuxOps() tmuxMover {
	if m.mover != nil {
		return m.mover
	}
	return m.tmuxClient
}

// dragRow follows a left-held pointer: once it leaves the pressed item the drag
// starts, and the sidebar marks where a drop would land.
func (m Model) dragRow(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	d := *m.rowDrag
	m.rowDrag = &d
	if !d.active {
		hit := m.sidebarHit(msg.X, msg.Y)
		if hit.Kind == ui.HitNone || hitIdentity(hit) == d.key {
			return m, nil
		}
		d.active = true
		m.pendingPress = nil
		m.lastClickID = ""
	}
	x, y := msg.X, msg.Y-lipgloss.Height(m.renderTitleBar())
	// Holding the pointer on the list's first or last line scrolls it.
	if top := y - 1; top <= 0 {
		m.sidebar.ScrollBy(-1)
	} else if top >= m.sidebar.ListHeight()-1 {
		m.sidebar.ScrollBy(1)
	}
	if d.pane != "" {
		d.sess, d.ok = m.sidebar.DragSessionOver(x, y, d.pane)
	} else {
		d.grp, d.ok = m.sidebar.DragGroupOver(x, y, d.group)
	}
	*m.rowDrag = d
	return m, nil
}

// hitIdentity names a hit item the way selectionIdentity names the selection.
func hitIdentity(hit ui.SidebarHit) string {
	if hit.Kind == ui.HitSession {
		return "s|" + clickID(hit.Session)
	}
	return "g|" + hit.Group
}

// finishRowDrag ends a press on a row or header: a drag drops, a plain click on
// a header toggles it, and a plain click on a row falls through to long-press.
func (m Model) finishRowDrag() (tea.Model, tea.Cmd, bool) {
	d := m.rowDrag
	m.rowDrag = nil
	m.sidebar.ClearDrop()
	switch {
	case d.active && d.ok && d.pane != "":
		model, cmd := m.dropSession(d.pane, d.sess)
		return model, cmd, true
	case d.active && d.ok:
		model, cmd := m.dropGroup(d.group, d.grp)
		return model, cmd, true
	case d.active:
		return m, nil, true
	case d.group != "" && m.sidebar.SelectedGroupName() == d.group:
		m.sidebar.ToggleGroupAtCursor()
		return m, m.afterSelectionChange(), true
	}
	return m, nil, false
}

// dropSession moves the dragged session's tmux window next to the drop's anchor
// row, or to the end of the drop's group.
func (m Model) dropSession(pane string, d ui.SessionDrop) (tea.Model, tea.Cmd) {
	m.useManualOrder(&m.cfg.SessionSortOrder, "session_sort_order")
	m.sidebar.ExpandGroup(d.Group)
	if d.Anchor != nil {
		dst, before := d.Anchor.TmuxPaneID, d.Before
		return m, m.moveWindowCmd(func(t tmuxMover) error { return t.MoveWindow(pane, dst, before) })
	}
	return m, m.moveWindowCmd(func(t tmuxMover) error { return t.MoveWindowToSession(pane, d.Group) })
}

// dropGroup moves group src to the drop and saves the new group order.
func (m Model) dropGroup(src string, d ui.GroupDrop) (tea.Model, tea.Cmd) {
	order := m.sidebar.MovedGroupOrder(src, d)
	if order == nil {
		return m, nil
	}
	m.sidebar.SetGroupOrder(order)
	m.useManualOrder(&m.cfg.GroupSortOrder, "group_sort_order")
	m.saveGroupOrder()
	m.sidebar.Rebuild()
	return m, nil
}

// moveWindowCmd runs a tmux window move off the event loop. The previewed window
// is restored first: the move changes the session:window targets it is tracked by.
func (m *Model) moveWindowCmd(op func(tmuxMover) error) tea.Cmd {
	m.restorePreviewedPane()
	m.previewTarget = "" // refit whatever the selection is after the move
	t := m.tmuxOps()
	return func() tea.Msg {
		return windowMovedMsg{err: op(t), curSess: detectCurrentTmuxSession(), curWin: detectCurrentTmuxWindow()}
	}
}

// applyWindowMoved records the outcome of a move or rename and refreshes the list.
func (m *Model) applyWindowMoved(msg windowMovedMsg) tea.Cmd {
	if msg.curSess != "" {
		m.currentTmuxSession, m.currentTmuxWin = msg.curSess, msg.curWin
	}
	if msg.err != nil {
		m.fail(msg.err)
	} else if msg.renamedFrom != "" {
		m.sidebar.RenameGroup(msg.renamedFrom, msg.renamedTo)
		m.saveGroupOrder()
	}
	return m.refreshSessionsCmd()
}

// useManualOrder switches a sort-order setting to "manual" so a drag sticks,
// saving the config and saying so once.
func (m *Model) useManualOrder(field *string, key string) {
	if *field == "manual" {
		return
	}
	*field = "manual"
	m.sidebar.SetGroupSortOrder(m.cfg.GroupSortOrder)
	m.sidebar.SetSessionSortOrder(m.cfg.SessionSortOrder)
	if err := m.writeConfig(); err != nil {
		m.statusbar.SetError(key + " is manual for this run only (config write failed: " + err.Error() + ")")
		return
	}
	m.statusbar.SetNotice(key + " set to manual")
}

func (m Model) writeConfig() error {
	save := m.saveConfig
	if save == nil {
		save = func(c config.Config) error { return config.Save(c, "") }
	}
	return save(m.cfg)
}

func (m Model) saveGroupOrder() {
	if m.viewState == nil {
		return
	}
	vs := m.viewState.Get()
	vs.GroupOrder = m.sidebar.GroupOrder()
	_ = m.viewState.Save(vs)
}

// ---------------------------------------------------------------------------
// Group header actions
// ---------------------------------------------------------------------------

// openGroupMenu selects the hit group header and shows its menu at (x, y).
func (m Model) openGroupMenu(hit ui.SidebarHit, x, y int) (tea.Model, tea.Cmd) {
	m.lastClickID = ""
	var cmd tea.Cmd
	if m.mode != ModeSearch {
		cmd = m.setFocus(ModeList)
	}
	m.sidebar.SetCursor(hit.Index)
	cmd = tea.Batch(cmd, m.afterSelectionChange())
	m.contextMenu.ShowGroup(x, y, hit.Group, m.sidebar.IsCollapsed(hit.Group))
	m.mode = ModeContextMenu
	m.statusbar.SetMode(ModeContextMenu.String())
	return m, cmd
}

func (m Model) executeGroupAction(action, group string) (tea.Model, tea.Cmd) {
	m.mode = ModeList
	m.statusbar.SetMode(ModeList.String())
	switch action {
	case "rename_group":
		return m.startRenameGroup(group)
	case "new_in_group":
		return m.createNewSession()
	case "toggle_group":
		if m.sidebar.SelectedGroupName() == group {
			m.sidebar.ToggleGroupAtCursor()
		}
	}
	return m, nil
}

// startRenameGroup opens the inline input to rename tmux session group.
func (m Model) startRenameGroup(group string) (tea.Model, tea.Cmd) {
	if group == "" || group == ui.ClosedGroupName {
		return m, nil
	}
	m.renameGroup = group
	m.mode = ModeRenameSession
	m.renameInput.SetSize(m.sidebarWidth())
	m.renameInput.SetPlaceholder("new tmux session name")
	cmd := m.renameInput.ActivateWithValue(group)
	m.statusbar.SetMode(ModeRenameSession.String())
	return m, cmd
}

// renameGroupCmd renames tmux session oldName to the typed name.
func (m *Model) renameGroupCmd(oldName, typed string) tea.Cmd {
	name := strings.TrimSpace(typed)
	if name == "" || name == oldName {
		return nil
	}
	if strings.ContainsAny(name, ":.") {
		m.statusbar.SetError("tmux session names can't contain ':' or '.'")
		return nil
	}
	m.restorePreviewedPane()
	m.previewTarget = ""
	t := m.tmuxOps()
	return func() tea.Msg {
		err := t.RenameSession(oldName, name)
		msg := windowMovedMsg{err: err, curSess: detectCurrentTmuxSession(), curWin: detectCurrentTmuxWindow()}
		if err == nil {
			msg.renamedFrom, msg.renamedTo = oldName, name
		}
		return msg
	}
}

// ---------------------------------------------------------------------------
// Move to session (context menu)
// ---------------------------------------------------------------------------

// openMovePicker lets the user pick (or name) the tmux session sess's window moves to.
func (m Model) openMovePicker(sess *session.Session) (tea.Model, tea.Cmd) {
	if sess == nil || sess.TmuxPaneID == "" {
		return m, nil
	}
	var names []string
	for _, n := range sessionNamesByRecency(m.tmuxClient.ListSessions()) {
		if n != sess.TmuxSession {
			names = append(names, n)
		}
	}
	m.pendingMoveSession = sess
	m.resumePicker.SetTitle(fmt.Sprintf("Move %q to session", sessionLabel(sess)))
	m.resumePicker.SetSize(m.width, m.height)
	m.resumePicker.Show(names, "")
	m.mode = ModeResumePicker
	m.statusbar.SetMode(ModeResumePicker.String())
	return m, m.resumePicker.Init()
}

func sessionLabel(s *session.Session) string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	if s.Slug != "" {
		return s.Slug
	}
	return s.ProjectName
}
