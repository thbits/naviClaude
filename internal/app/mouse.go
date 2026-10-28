package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/ui"
)

const (
	// doubleClickWindow is the max gap between two presses on the same row
	// for them to count as a double-click.
	doubleClickWindow = 400 * time.Millisecond

	// longPressThreshold is the hold that opens a row's context menu, standing
	// in for right-click, which a tmux root MouseDown3Pane binding swallows.
	longPressThreshold = 450 * time.Millisecond
)

// mousePress is a left press on a session row awaiting its release.
type mousePress struct {
	id   string
	x, y int
	at   time.Time
}

func (m Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// clickID identifies a session row across re-renders; the target covers
// placeholders that have no session ID yet.
func clickID(s *session.Session) string {
	return s.Key() + "|" + s.TmuxTarget
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionRelease:
		return m.handleMouseRelease(msg)
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonLeft && m.draggingSidebar {
			return m.dragSidebar(msg.X)
		}
		if msg.Button == tea.MouseButtonLeft && m.preview.Selecting() {
			m.preview.ExtendSelection(m.previewPoint(msg.X, msg.Y))
			return m, nil
		}
		if msg.Button == tea.MouseButtonNone {
			return m.hover(msg)
		}
		return m, nil
	case tea.MouseActionPress:
	default:
		return m, nil
	}

	// Any other button event between a press and its release cancels a
	// pending long press or preview drag.
	m.pendingPress = nil
	m.draggingSidebar = false
	m.preview.ClearSelection()

	// Inline inputs and the kill prompt keep ignoring the mouse; a click must
	// never act on what sits underneath them.
	if m.confirmKill || m.nameInput.IsActive() || m.renameInput.IsActive() {
		return m, nil
	}

	// A mouse interaction also means the user is in control; cancel any pending
	// startup restore so it cannot later steal focus (mirrors handleKey).
	m.restoredLastSession = true
	m.statusbar.ClearError()

	if m.overlayOpen() {
		return m.handleOverlayMouse(msg)
	}

	if msg.Y >= m.statusBarTop() {
		if msg.Button == tea.MouseButtonLeft && msg.Y == m.height-1 {
			return m.clickStatusHint(msg.X)
		}
		return m, nil
	}

	sidebarWidth := m.sidebarWidth()
	rightWidth := m.rightSidebarWidth()
	rightPaneLeft := m.width - rightWidth // left edge of the changed-files region

	switch msg.Button {
	case tea.MouseButtonLeft:
		switch {
		case msg.X == sidebarWidth-1:
			// The sidebar's right border is a drag handle.
			m.draggingSidebar = true
			return m, nil
		case msg.X < sidebarWidth:
			return m.leftPressSidebar(msg)
		case rightWidth > 0 && msg.X >= rightPaneLeft:
			cmd := m.setFocus(ModeChangedFiles)
			return m, cmd
		default:
			// Preview band: the release decides between a click and a drag.
			m.preview.StartSelection(m.previewPoint(msg.X, msg.Y))
			return m, nil
		}

	case tea.MouseButtonRight:
		if msg.X < sidebarWidth {
			return m.rightPressSidebar(msg)
		}
		return m, nil

	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		switch {
		case rightWidth > 0 && msg.X >= rightPaneLeft:
			var cmd tea.Cmd
			m.rightSidebar, cmd = m.rightSidebar.Update(msg)
			return m, cmd
		case msg.X < sidebarWidth:
			return m.wheelSidebar(msg.Button == tea.MouseButtonWheelUp)
		default:
			if cmd, ok := m.forwardWheel(msg.Button == tea.MouseButtonWheelUp, msg.X, msg.Y); ok {
				return m, cmd
			}
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// hover highlights the sidebar row under the pointer, or moves an open context
// menu's cursor to the item under it, as a desktop menu does.
func (m Model) hover(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.contextMenu.IsVisible() {
		if i, ok := m.contextMenu.ItemAt(msg.X, msg.Y); ok {
			m.contextMenu.SetCursor(i)
		}
		return m, nil
	}
	idx := -1
	if !m.overlayOpen() && msg.X < m.sidebarWidth() && msg.Y < m.statusBarTop() {
		if hit := m.sidebarHit(msg.X, msg.Y); hit.Kind != ui.HitNone {
			idx = hit.Index
		}
	}
	m.sidebar.SetHover(idx)
	return m, nil
}

// handleMouseRelease turns a left press held on the same session row for at
// least longPressThreshold into the context menu, as right-click would.
func (m Model) handleMouseRelease(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.draggingSidebar {
		return m.finishSidebarDrag(msg.X)
	}
	if m.preview.Selecting() {
		return m.finishPreviewSelection(msg)
	}
	p := m.pendingPress
	m.pendingPress = nil
	if p == nil || m.overlayOpen() || m.mode == ModeSearch ||
		m.confirmKill || m.nameInput.IsActive() || m.renameInput.IsActive() {
		return m, nil
	}
	if m.clock().Sub(p.at) < longPressThreshold {
		return m, nil
	}
	hit := m.sidebarHit(msg.X, msg.Y)
	if hit.Kind != ui.HitSession || clickID(hit.Session) != p.id {
		return m, nil
	}
	return m.openContextMenu(hit, p.x, p.y)
}

// dragSidebar moves the sidebar border to column x. The previewed tmux window
// is refit only on release, so a drag doesn't run a tmux resize per cell.
func (m Model) dragSidebar(x int) (tea.Model, tea.Cmd) {
	m.sidebarCols = x + 1
	m.sidebarCols = m.sidebarWidth() // store the clamped width
	m.resizeComponents()
	return m, nil
}

// finishSidebarDrag applies the last drag position, refits the previewed pane
// and saves the width to the config as a percentage.
func (m Model) finishSidebarDrag(x int) (tea.Model, tea.Cmd) {
	model, _ := m.dragSidebar(x)
	m = model.(Model)
	m.draggingSidebar = false
	m.resizePreviewedPane()
	if m.width <= 0 {
		return m, nil
	}
	pct := (m.sidebarWidth()*100 + m.width/2) / m.width
	if pct == m.cfg.SidebarWidth {
		return m, nil
	}
	m.cfg.SidebarWidth = pct
	m.sidebarWidthPct = pct
	save := m.saveConfig
	if save == nil {
		save = func(c config.Config) error { return config.Save(c, "") }
	}
	if err := save(m.cfg); err != nil {
		m.statusbar.SetError("sidebar width kept for this session only (config write failed: " + err.Error() + ")")
	}
	return m, nil
}

// finishPreviewSelection copies a drag's text to the clipboard; a release where
// the press landed is a click, which focuses the preview.
func (m Model) finishPreviewSelection(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.preview.ExtendSelection(m.previewPoint(msg.X, msg.Y))
	text, dragged := m.preview.EndSelection()
	if !dragged {
		// Focus passthrough only if the selection can receive forwarded keys
		// (mirrors nextFocusMode's canPassthrough).
		sel := m.sidebar.SelectedSession()
		if sel != nil && sel.Status != session.StatusClosed && sel.TmuxTarget != "" {
			return m, m.setFocus(ModePassthrough)
		}
		return m, nil
	}
	if strings.TrimSpace(text) == "" {
		return m, nil
	}
	write := m.copyText
	if write == nil {
		write = copyToClipboard
	}
	lines := strings.Count(text, "\n") + 1
	return m, func() tea.Msg { return clipboardCopiedMsg{lines: lines, err: write(text)} }
}

// clipboardCopiedMsg reports a preview selection copy.
type clipboardCopiedMsg struct {
	lines int
	err   error
}

// previewPoint converts a terminal cell to coordinates relative to the preview.
func (m Model) previewPoint(x, y int) (int, int) {
	return x - m.sidebarWidth(), y - lipgloss.Height(m.renderTitleBar())
}

// statusBarTop is the first terminal row of the status bar.
func (m Model) statusBarTop() int {
	return m.height - lipgloss.Height(m.statusbar.View())
}

// sidebarHit maps a terminal cell to the sidebar item under it, preparing a
// sidebar copy as View() does (focus, search input) so the hit map matches.
func (m Model) sidebarHit(x, y int) ui.SidebarHit {
	sb := m.sidebar
	sb.SetFocused(m.focusedPane() == PaneList)
	titleH := lipgloss.Height(m.renderTitleBar())
	y -= titleH
	if m.search.IsActive() {
		searchH := lipgloss.Height(m.search.View())
		y -= searchH
		sb.SetSize(m.sidebarWidth()-1, m.statusBarTop()-titleH-searchH)
	}
	return sb.HitTest(x, y)
}

func (m Model) leftPressSidebar(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	hit := m.sidebarHit(msg.X, msg.Y)
	switch hit.Kind {
	case ui.HitSession:
		return m.clickSession(hit, msg)
	case ui.HitGroup:
		// Search keeps every group expanded, so a toggle there is meaningless.
		if m.mode == ModeSearch {
			return m, nil
		}
		m.lastClickID = ""
		cmd := m.setFocus(ModeList)
		m.sidebar.SetCursor(hit.Index)
		m.sidebar.ToggleGroupAtCursor()
		return m, tea.Batch(cmd, m.afterSelectionChange())
	default:
		if m.mode == ModeSearch {
			return m, nil
		}
		return m, m.setFocus(ModeList)
	}
}

// clickSession selects the clicked row; a second press on the same row within
// doubleClickWindow acts as Enter.
func (m Model) clickSession(hit ui.SidebarHit, msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	now := m.clock()
	id := clickID(hit.Session)
	double := id == m.lastClickID && now.Sub(m.lastClickAt) <= doubleClickWindow

	var cmds []tea.Cmd
	searching := m.mode == ModeSearch
	if !searching {
		cmds = append(cmds, m.setFocus(ModeList))
	}
	m.sidebar.SetCursor(hit.Index)
	cmds = append(cmds, m.afterSelectionChange())

	if double {
		m.lastClickID = ""
		enter := tea.KeyMsg{Type: tea.KeyEnter}
		var next tea.Model
		var cmd tea.Cmd
		if searching {
			next, cmd = m.handleSearchKey(enter)
		} else {
			next, cmd = m.handleListKey(enter)
		}
		return next, tea.Batch(append(cmds, cmd)...)
	}

	m.lastClickID, m.lastClickAt = id, now
	if !searching {
		m.pendingPress = &mousePress{id: id, x: msg.X, y: msg.Y, at: now}
	}
	return m, tea.Batch(cmds...)
}

// rightPressSidebar opens the context menu for the clicked session row.
func (m Model) rightPressSidebar(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	hit := m.sidebarHit(msg.X, msg.Y)
	if hit.Kind != ui.HitSession {
		return m, nil
	}
	return m.openContextMenu(hit, msg.X, msg.Y)
}

// openContextMenu selects the hit row and shows its menu at (x, y).
func (m Model) openContextMenu(hit ui.SidebarHit, x, y int) (tea.Model, tea.Cmd) {
	m.lastClickID = ""
	var cmd tea.Cmd
	if m.mode != ModeSearch {
		cmd = m.setFocus(ModeList)
	}
	m.sidebar.SetCursor(hit.Index)
	cmd = tea.Batch(cmd, m.afterSelectionChange())
	m.contextMenu.Show(x, y, hit.Session, m.menuCaps(hit.Session))
	m.mode = ModeContextMenu
	m.statusbar.SetMode(ModeContextMenu.String())
	return m, cmd
}

// wheelSidebar moves the selection one row per wheel tick, like j/k.
func (m Model) wheelSidebar(up bool) (tea.Model, tea.Cmd) {
	if m.mode == ModeSearch {
		key := tea.KeyMsg{Type: tea.KeyDown}
		if up {
			key = tea.KeyMsg{Type: tea.KeyUp}
		}
		return m.handleSearchKey(key)
	}
	var cmd tea.Cmd
	// Leave passthrough first so forwarded keys never silently retarget.
	if m.mode == ModePassthrough {
		cmd = m.setFocus(ModeList)
	}
	delta := 1
	if up {
		delta = -1
	}
	prev := m.selectionIdentity()
	m.sidebar.MoveCursor(delta)
	return m, tea.Batch(cmd, m.selectionMoved(prev))
}

// ---------------------------------------------------------------------------
// Status bar
// ---------------------------------------------------------------------------

// keyTypesByName maps bubbletea key names ("enter", "ctrl+]") to key types so
// a status-bar label can be replayed as a key press.
var keyTypesByName = func() map[string]tea.KeyType {
	out := make(map[string]tea.KeyType)
	for kt := tea.KeyType(-256); kt <= 127; kt++ {
		if kt == tea.KeyRunes {
			continue
		}
		if name := (tea.Key{Type: kt}).String(); name != "" {
			if _, dup := out[name]; !dup {
				out[name] = kt
			}
		}
	}
	return out
}()

// hintKeyMsg converts a status-bar key label to the key press it names.
// Single characters stay case-sensitive (K and k are different actions).
func hintKeyMsg(label string) (tea.KeyMsg, bool) {
	if label == "any key" {
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	}
	if r := []rune(label); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	if kt, ok := keyTypesByName[label]; ok {
		return tea.KeyMsg{Type: kt}, true
	}
	if kt, ok := keyTypesByName[strings.ToLower(label)]; ok {
		return tea.KeyMsg{Type: kt}, true
	}
	return tea.KeyMsg{}, false
}

func (m Model) statusHintKey(x int) (tea.KeyMsg, bool) {
	label, ok := m.statusbar.HintAt(x)
	if !ok {
		return tea.KeyMsg{}, false
	}
	return hintKeyMsg(label)
}

// clickStatusHint replays the clicked hint's key in a non-overlay mode.
func (m Model) clickStatusHint(x int) (tea.Model, tea.Cmd) {
	k, ok := m.statusHintKey(x)
	if !ok {
		return m, nil
	}
	switch m.mode {
	case ModeList, ModeChangedFiles, ModePassthrough, ModeSearch:
		// Each shows its own hints, so the key acts as in that mode.
		return m.handleKey(k)
	}
	return m, nil
}

// overlayHintKey returns the status-line hint clicked while an overlay showing
// its own hints is open; other overlays sit above list hints that must not fire.
func (m Model) overlayHintKey(msg tea.MouseMsg) (tea.KeyMsg, bool) {
	if msg.Button != tea.MouseButtonLeft || msg.Y != m.height-1 {
		return tea.KeyMsg{}, false
	}
	switch m.mode {
	case ModeContextMenu, ModeThemePicker, ModeDetail, ModeStats:
		return m.statusHintKey(msg.X)
	}
	return tea.KeyMsg{}, false
}

// onStatusHint reports a left press on any status-line hint, replayable or not.
func (m Model) onStatusHint(msg tea.MouseMsg) bool {
	return msg.Button == tea.MouseButtonLeft && msg.Y == m.height-1 && m.statusbar.OnHint(msg.X)
}

// ---------------------------------------------------------------------------
// Overlays
// ---------------------------------------------------------------------------

// overlayOpen reports whether a popup or menu owns the mouse.
func (m Model) overlayOpen() bool {
	return m.contextMenu.IsVisible() ||
		m.help.IsVisible() ||
		m.detail.IsVisible() ||
		m.resumePicker.IsVisible() ||
		m.dirPicker.IsVisible() ||
		m.themePicker.IsVisible() ||
		m.statsModel.IsVisible()
}

// handleOverlayMouse routes to the top-most overlay, in View's compositing
// order, so a click never reaches a pane underneath.
func (m Model) handleOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.contextMenu.IsVisible():
		return m.contextMenuMouse(msg)
	case m.help.IsVisible():
		if m.clickedOutside(msg, m.help.View()) {
			if k, ok := m.overlayHintKey(msg); ok {
				return m.handleKey(k)
			}
			if m.onStatusHint(msg) {
				return m, nil // inert hint such as j/k: not a click outside
			}
			m.help.Hide()
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
		}
		return m, nil
	case m.detail.IsVisible():
		if m.clickedOutside(msg, m.detail.View()) {
			if k, ok := m.overlayHintKey(msg); ok {
				return m.handleKey(k)
			}
			if m.onStatusHint(msg) {
				return m, nil // inert hint such as j/k: not a click outside
			}
			return m.handleDetailKey(tea.KeyMsg{Type: tea.KeyEsc})
		}
		return m, nil
	case m.resumePicker.IsVisible():
		return m.resumePickerMouse(msg)
	case m.dirPicker.IsVisible():
		return m.dirPickerMouse(msg)
	case m.themePicker.IsVisible():
		return m.themePickerMouse(msg)
	case m.statsModel.IsVisible():
		return m.statsMouse(msg)
	}
	return m, nil
}

// clickedOutside reports a left/right press outside the centered popup view.
func (m Model) clickedOutside(msg tea.MouseMsg, view string) bool {
	if msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight {
		return false
	}
	x, y, w, h := ui.OverlayRect(m.width, m.height, view)
	return !ui.InRect(msg.X, msg.Y, x, y, w, h)
}

func (m Model) contextMenuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonLeft:
		if i, ok := m.contextMenu.ItemAt(msg.X, msg.Y); ok {
			m.contextMenu.SetCursor(i)
			return m.runContextMenuSelection()
		}
		if m.contextMenu.Contains(msg.X, msg.Y) {
			return m, nil
		}
		if k, ok := m.overlayHintKey(msg); ok {
			return m.handleKey(k)
		}
		if m.onStatusHint(msg) {
			return m, nil // inert hint such as j/k: not a click outside
		}
		m.closeContextMenu()
		return m, nil

	case tea.MouseButtonRight:
		if m.contextMenu.Contains(msg.X, msg.Y) {
			return m, nil
		}
		m.closeContextMenu()
		// Right-clicking another row re-opens the menu there.
		if msg.X < m.sidebarWidth() && msg.Y < m.statusBarTop() {
			return m.rightPressSidebar(msg)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) statsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight {
		return m, nil
	}
	view := m.statsModel.View()
	x, y, w, h := ui.OverlayRect(m.width, m.height, view)
	if ui.InRect(msg.X, msg.Y, x, y, w, h) {
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if f, ok := m.statsModel.FilterAt(msg.X-x, msg.Y-y); ok && m.statsModel.SetFilter(f) {
			return m, m.computeStatsCmd()
		}
		return m, nil
	}
	if k, ok := m.overlayHintKey(msg); ok {
		return m.handleKey(k)
	}
	if m.onStatusHint(msg) {
		return m, nil // inert hint such as j/k: not a click outside
	}
	return m.handleStatsKey(tea.KeyMsg{Type: tea.KeyEsc})
}

// pickerAction is what a mouse press means to a list picker popup.
type pickerAction int

const (
	pickNone pickerAction = iota
	pickSelect
	pickConfirm
	pickCancel
	pickUp
	pickDown
)

// classifyPickerMouse maps a press over a centered list popup to an action.
// Clicking the already-highlighted row confirms, so a double-click confirms.
func (m Model) classifyPickerMouse(msg tea.MouseMsg, view string, rowAt func(int) (int, bool), cursor int) (pickerAction, int) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return pickUp, 0
	case tea.MouseButtonWheelDown:
		return pickDown, 0
	case tea.MouseButtonLeft, tea.MouseButtonRight:
	default:
		return pickNone, 0
	}
	x, y, w, h := ui.OverlayRect(m.width, m.height, view)
	if !ui.InRect(msg.X, msg.Y, x, y, w, h) {
		return pickCancel, 0
	}
	if msg.Button != tea.MouseButtonLeft {
		return pickNone, 0
	}
	row, ok := rowAt(msg.Y - y)
	if !ok {
		return pickNone, 0
	}
	if row == cursor {
		return pickConfirm, row
	}
	return pickSelect, row
}

func (m Model) themePickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	action, row := m.classifyPickerMouse(msg, m.themePicker.View(), m.themePicker.RowAt, m.themePicker.Cursor())
	switch action {
	case pickSelect:
		// Preview live, as keyboard navigation does.
		if key, ok := m.themePicker.SetCursor(row); ok {
			styles.ApplyTheme(styles.Named(key))
		}
	case pickConfirm:
		return m.handleThemePickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	case pickCancel:
		if k, ok := m.overlayHintKey(msg); ok {
			return m.handleKey(k)
		}
		if m.onStatusHint(msg) {
			return m, nil // inert hint such as j/k: not a click outside
		}
		return m.handleThemePickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	case pickUp:
		return m.handleThemePickerKey(tea.KeyMsg{Type: tea.KeyUp})
	case pickDown:
		return m.handleThemePickerKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	return m, nil
}

func (m Model) dirPickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	action, row := m.classifyPickerMouse(msg, m.dirPicker.View(), m.dirPicker.RowAt, m.dirPicker.Cursor())
	switch action {
	case pickSelect:
		m.dirPicker.SetCursor(row)
	case pickConfirm:
		return m.handleDirPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	case pickCancel:
		return m.handleDirPickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	case pickUp:
		m.dirPicker.MoveUp()
	case pickDown:
		m.dirPicker.MoveDown()
	}
	return m, nil
}

func (m Model) resumePickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	action, row := m.classifyPickerMouse(msg, m.resumePicker.View(), m.resumePicker.RowAt, m.resumePicker.Cursor())
	switch action {
	case pickSelect:
		m.resumePicker.SetCursor(row)
	case pickConfirm:
		return m.handleResumePickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	case pickCancel:
		return m.handleResumePickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	case pickUp:
		m.resumePicker.MoveUp()
	case pickDown:
		m.resumePicker.MoveDown()
	}
	return m, nil
}
