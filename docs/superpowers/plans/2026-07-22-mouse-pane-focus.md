# Mouse Pane-Focus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Left-clicking a layout pane gives it keyboard focus through the same `setFocus` path the keyboard uses, and the changed-files pane scrolls on the wheel.

**Architecture:** Rewrite `Model.handleMouse` in `internal/app/app.go` to map a click's X column to one of the three panes and call `setFocus`, guarded so clicks are ignored while a modal/picker/input is open. Add wheel-scroll to `ChangedFilesModel` so wheel-over-right-pane works.

**Tech Stack:** Go, Bubble Tea (`github.com/charmbracelet/bubbletea`), Lipgloss.

**Status:** COMPLETE. Task 1 = commit 0541d1e. Task 2 impl was swept into concurrent commit 5312889 (byte-identical to Step 3); test file = 6045361. Both task reviews and the final whole-feature review came back clean; both suites green. See `.superpowers/sdd/progress.md` for the ledger and the concurrency incident note.

## Global Constraints

- No new dependencies.
- No emojis in code, comments, or output.
- Follow existing file patterns: value-receiver `Update`/`handleMouse` methods; `setFocus` has a pointer receiver and is called on the addressable local `m`.
- Spec: `docs/superpowers/specs/2026-07-22-mouse-pane-focus-design.md`.

---

### Task 1: Wheel-scroll the changed-files pane

**Files:**
- Modify: `internal/ui/changedfiles.go` (the `Update` method)
- Test: `internal/ui/changedfiles_wheel_test.go` (create)

**Interfaces:**
- Consumes: existing `ChangedFilesModel` with `cursor int`, `files []session.ChangedFile`, `syncViewport()`, `SelectedFile() string`, `SetFiles(files, cwd)`.
- Produces: `ChangedFilesModel.Update` now moves the cursor on `tea.MouseWheelUp` / `tea.MouseWheelDown`, matching `k` / `j`.

- [x] **Step 1: Write the failing test** (`internal/ui/changedfiles_wheel_test.go`): `TestChangedFilesWheelMovesCursor` (wheel down/up moves selection) and `TestChangedFilesWheelClamps` (no wrap at bounds), building a real model via `NewChangedFiles`/`SetFiles` and driving `Update(tea.MouseMsg{Type: tea.MouseWheelDown/Up})`.
- [x] **Step 2: Run to verify it fails** — `go test ./internal/ui/ -run TestChangedFilesWheel -v`, expect FAIL (cursor does not move).
- [x] **Step 3: Add a `tea.MouseMsg` case to `Update`** alongside the existing `tea.KeyMsg` case:

```go
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
			m.syncViewport()
		case tea.MouseWheelDown:
			if m.cursor < len(m.files)-1 {
				m.cursor++
			}
			m.syncViewport()
		}
```

- [x] **Step 4: Run to verify it passes** — `go test ./internal/ui/ -run TestChangedFilesWheel -v`, expect PASS.
- [x] **Step 5: Commit** — `feat(ui): scroll the changed-files pane on mouse wheel`.

---

### Task 2: Route left-clicks through the pane-focus model

**Files:**
- Modify: `internal/app/app.go` (replace `handleMouse`; add a `modalActive` helper near it).
- Test: `internal/app/mouse_focus_test.go` (create)

**Interfaces:**
- Consumes: `setFocus(target Mode) tea.Cmd` (pointer receiver), `sidebarWidth() int`, `rightSidebarWidth() int`, `focusedPane() Pane`, `sidebar.SelectedSession()`, `sidebar.SelectByTarget(target)`, `rightSidebar.Update(msg)` (wheel from Task 1), `preview.Update(msg)`, `contextMenu.Show(x, y, sess)`; visibility predicates `help.IsVisible()`, `statsModel.IsVisible()`, `themePicker.IsVisible()`, `dirPicker.IsVisible()`, `resumePicker.IsVisible()`, `detail.IsVisible()`, `contextMenu.IsVisible()`, `nameInput.IsActive()`, `renameInput.IsActive()`; field `confirmKill bool`.
- Produces: `handleMouse` that sets pane focus via `setFocus`; a `modalActive() bool` helper.

- [x] **Step 1: Write the failing tests** (`internal/app/mouse_focus_test.go`): a `mouseModel(rightOpen bool)` helper (reusing the existing `newFocusModel`, width=100 -> sidebarWidth 20, right pane [76,100) when open), and `TestMouseClickPreviewEntersPassthrough`, `TestMouseClickSidebarFocusesList`, `TestMouseClickRightPaneFocusesChangedFiles`, `TestMouseClickRightEdgeWithPanelClosedIsPreview`, `TestMouseClickPreviewNoSelectionNoop`, `TestMouseIgnoredWhenModalActive`. Each asserts `mode` and `focusedPane()`.
- [x] **Step 2: Run to verify they fail** — `go test ./internal/app/ -run TestMouse -v` (sidebar/right-pane/modal-guard tests fail against the old `handleMouse`).
- [x] **Step 3: Replace `handleMouse` and add `modalActive`:**

```go
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Ignore mouse input while a modal overlay, picker, or inline input owns the
	// screen -- a click must not silently re-focus a pane hidden underneath it.
	// Search is intentionally excluded (see modalActive): it stacks above the
	// list and keeps list focus, so a sidebar click during search is normal.
	if m.modalActive() {
		return m, nil
	}

	sidebarWidth := m.sidebarWidth()
	rightWidth := m.rightSidebarWidth()
	rightPaneLeft := m.width - rightWidth // left edge of the changed-files region

	switch msg.Type {
	case tea.MouseLeft:
		switch {
		case msg.X < sidebarWidth:
			cmd := m.setFocus(ModeList)
			return m, cmd
		case rightWidth > 0 && msg.X >= rightPaneLeft:
			cmd := m.setFocus(ModeChangedFiles)
			return m, cmd
		default:
			// Preview band: focus passthrough only if the selection can receive
			// forwarded keys (mirrors nextFocusMode's canPassthrough); otherwise
			// leave focus unchanged.
			sel := m.sidebar.SelectedSession()
			if sel != nil && sel.Status != session.StatusClosed && sel.TmuxTarget != "" {
				cmd := m.setFocus(ModePassthrough)
				return m, cmd
			}
			return m, nil
		}

	case tea.MouseRight:
		if msg.X < sidebarWidth {
			if sess := m.sidebar.SelectedSession(); sess != nil {
				m.contextMenu.Show(msg.X, msg.Y, sess)
				m.mode = ModeContextMenu
				m.statusbar.SetMode(ModeContextMenu.String())
			}
		}
		return m, nil

	case tea.MouseWheelUp, tea.MouseWheelDown:
		switch {
		case rightWidth > 0 && msg.X >= rightPaneLeft:
			var cmd tea.Cmd
			m.rightSidebar, cmd = m.rightSidebar.Update(msg)
			return m, cmd
		case msg.X < sidebarWidth:
			// The session list does not scroll on wheel; no-op.
			return m, nil
		default:
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// modalActive reports whether a modal overlay, picker, or inline input owns the
// screen, in which case mouse clicks must not re-focus panes underneath it.
// Search is deliberately excluded: it keeps list focus, so clicks stay valid.
func (m Model) modalActive() bool {
	return m.confirmKill ||
		m.help.IsVisible() ||
		m.statsModel.IsVisible() ||
		m.themePicker.IsVisible() ||
		m.dirPicker.IsVisible() ||
		m.resumePicker.IsVisible() ||
		m.detail.IsVisible() ||
		m.contextMenu.IsVisible() ||
		m.nameInput.IsActive() ||
		m.renameInput.IsActive()
}
```

- [x] **Step 4: Run to verify they pass** — `go test ./internal/app/ -run TestMouse -v` (all six).
- [x] **Step 5: Full package suites** — `go test ./internal/app/ ./internal/ui/` (no regressions).
- [x] **Step 6: Commit** — `feat(ui): left-click focuses a pane via setFocus; guard modals`.

---

## Manual verification

- [ ] `go build ./... && ./naviclaude` inside tmux with at least one active session.
- [ ] Click the session list, the preview, and (with the changed-files pane open) the right pane; confirm focus moves to match Tab.
- [ ] Click the preview while a closed session is selected: focus does not jump.
- [ ] Open help (`?`), click a pane: nothing re-focuses underneath.
- [ ] Wheel over the changed-files pane: selection scrolls.

## Known minor (non-blocking, from final review)

- `TestMouseClickPreviewNoSelectionNoop` covers only the `sel == nil` no-op branch; the `StatusClosed` and empty-`TmuxTarget` branches are untested (verbatim from the brief). Cheap table-driven extension if desired.
