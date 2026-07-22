# Active-Pane Focus Clarity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make it unmistakable at a glance which of the three panes (session list, preview, changed-files) is active.

**Architecture:** One derived `focusedPane()` value drives three coordinated, palette-driven signals: a lit reverse-video title bar on the active pane, a thick blue separator edge owned by the active pane (list/files only), and dimmed content on inactive panes (sidebar keeps its status dots colored; preview body stays bright). No layout/width change.

**Tech Stack:** Go 1.26, Bubble Tea, Lipgloss. Styles are built from a `styles.Palette` in `buildStyles`.

## Global Constraints

- No emojis anywhere (output or code). Geometric glyphs like `▸` are allowed and already used (`▼ ▶ ● ◎`).
- Every color MUST derive from the active `styles.Palette` role (`ColorBlue`, `ColorBorder`, `ColorDimText`, `ColorBg`, `ColorGreen`, `ColorRed`) — never a hardcoded hex. This is what makes it work across all 11 themes.
- No terminal-width cost: the active separator swaps the border *rune* (thick `┃` vs thin `│`), both 1 cell wide; it never adds/removes a column.
- Preview body (live tmux ANSI) is never dimmed — only its header chip + underline change with focus.
- Sidebar status dots keep their semantic colors even when the sidebar is dimmed.
- Run `gofmt` and keep the existing style/test conventions in each file.

---

### Task 1: Focus styles (palette-driven title bars + thick active edges)

**Files:**
- Modify: `internal/styles/styles.go` (declarations near lines 45-58, 108-128; assignments in `buildStyles` near lines 319-341, 409-415)
- Modify: `internal/styles/styles_buildstyles_test.go:71-72,93-94` (update two changed styles; add the two new ones)
- Test: `internal/styles/focus_contrast_test.go` (new)

**Interfaces:**
- Produces: `styles.PaneTitleActive`, `styles.PaneTitleInactive` (lipgloss.Style); `styles.SidebarPanelFocused`, `styles.RightSidebarPanelFocused`, `styles.PreviewHeaderFocused` now use `lipgloss.ThickBorder()`.

- [ ] **Step 1: Write the failing test for the palette invariant**

Create `internal/styles/focus_contrast_test.go`:

```go
package styles

import "testing"

// The active-pane separator uses Blue and the inactive one uses Border. If a
// theme set them equal the focus edge would be invisible, so guard every theme.
func TestFocusAccentContrastsWithBorderInAllThemes(t *testing.T) {
	for name, p := range Themes {
		if p.Blue == p.Border {
			t.Errorf("theme %q: Blue (%s) == Border (%s); active vs inactive edge would not contrast",
				name, p.Blue, p.Border)
		}
	}
}

// The active title bar paints Blue as background with Bg as foreground; if a
// theme set them equal the title text would vanish on its own bar.
func TestActiveTitleBarHasContrastInAllThemes(t *testing.T) {
	for name, p := range Themes {
		if p.Blue == p.Bg {
			t.Errorf("theme %q: Blue (%s) == Bg (%s); active title text would be invisible",
				name, p.Blue, p.Bg)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it passes already (guards existing palettes)**

Run: `go test ./internal/styles/ -run TestFocusAccent -v` and `go test ./internal/styles/ -run TestActiveTitleBar -v`
Expected: PASS (all 11 themes already satisfy `Blue != Border` and `Blue != Bg`). This test is a regression guard for future palette edits.

- [ ] **Step 3: Add the two new style declarations**

In `internal/styles/styles.go`, after the `RightSidebarPanelFocused` declaration (around line 58), add:

```go
// PaneTitleActive is the lit reverse-video title bar shown on the focused pane
// (blue background, bg-colored text). Derives from the palette so it follows
// every theme, including light ones (fg=Bg keeps contrast on the blue bar).
var PaneTitleActive lipgloss.Style

// PaneTitleInactive is the dim title shown on an unfocused pane.
var PaneTitleInactive lipgloss.Style
```

- [ ] **Step 4: Assign the new styles and thicken the focused edges in buildStyles**

In `internal/styles/styles.go`, change the three existing assignments and add the two new ones. Replace the `SidebarPanelFocused` assignment (around lines 324-327) with:

```go
	SidebarPanelFocused = lipgloss.NewStyle().
		BorderRight(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorBlue)
```

Replace the `RightSidebarPanelFocused` assignment (around lines 337-340) with:

```go
	RightSidebarPanelFocused = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorBlue)
```

Replace the `PreviewHeaderFocused` assignment (around lines 409-415) with:

```go
	PreviewHeaderFocused = lipgloss.NewStyle().
		Foreground(ColorFg).
		PaddingLeft(1).
		PaddingRight(1).
		BorderBottom(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorBlue)
```

Then, immediately after the `PreviewHeaderFocused` assignment, add:

```go
	PaneTitleActive = lipgloss.NewStyle().
		Background(ColorBlue).
		Foreground(ColorBg).
		Bold(true)

	PaneTitleInactive = lipgloss.NewStyle().
		Foreground(ColorDimText).
		PaddingLeft(1)
```

- [ ] **Step 5: Update the buildstyles equivalence test**

In `internal/styles/styles_buildstyles_test.go`, update the `want` entry for `SidebarPanelFocused` (lines 71-72) to:

```go
		"SidebarPanelFocused": lipgloss.NewStyle().BorderRight(true).
			BorderStyle(lipgloss.ThickBorder()).BorderForeground(cBlue),
```

Update the `want` entry for `PreviewHeaderFocused` (lines 93-94) to:

```go
		"PreviewHeaderFocused": lipgloss.NewStyle().Foreground(cFg).PaddingLeft(1).PaddingRight(1).
			BorderBottom(true).BorderStyle(lipgloss.ThickBorder()).BorderForeground(cBlue),
```

Add two new `want` entries (anywhere in the map, e.g. after `SidebarTitleCount`):

```go
		"PaneTitleActive":   lipgloss.NewStyle().Background(cBlue).Foreground(cBg).Bold(true),
		"PaneTitleInactive": lipgloss.NewStyle().Foreground(cDimText).PaddingLeft(1),
```

Add the matching `got` entries (in the `got` map near line 163):

```go
		"PaneTitleActive":   PaneTitleActive,
		"PaneTitleInactive": PaneTitleInactive,
```

Note: `cDimText` and `cBg` are already declared locals in this test (used elsewhere in the map). If a needed local is missing, add it mirroring the others at the top of the test.

- [ ] **Step 6: Run the styles tests**

Run: `go test ./internal/styles/ -v`
Expected: PASS (buildstyles equivalence + both contrast guards).

- [ ] **Step 7: Commit**

```bash
git add internal/styles/styles.go internal/styles/styles_buildstyles_test.go internal/styles/focus_contrast_test.go
git commit -m "feat(styles): palette-driven focus title bars and thick active edges"
```

---

### Task 2: Sidebar focus rendering (lit title, dim content, keep dots)

**Files:**
- Modify: `internal/ui/sidebar.go` (struct near line 42-73; `NewSidebar` near line 76; new `SetFocused`; `View` lines 668-701; `renderGroupHeader` lines 745-756; `renderSessionItem` normal branch lines 879-895)
- Test: `internal/ui/sidebar_focus_test.go` (new)

**Interfaces:**
- Consumes: `styles.PaneTitleActive`, `styles.PaneTitleInactive`, `styles.ColorDimText` (from Task 1 / existing palette).
- Produces: `(*SidebarModel).SetFocused(bool)`; sidebar renders a lit `▸ SESSIONS` bar and dimmed non-selected content when unfocused.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/sidebar_focus_test.go`:

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

func sidebarWithOneSession() SidebarModel {
	m := NewSidebar(30, 12)
	m.SetSessions([]*session.Session{{
		ID:           "abcd1234",
		TmuxSession:  "work",
		TmuxTarget:   "work:1.0",
		ProjectName:  "myproj",
		Status:       session.StatusActive,
		LastActivity: time.Now(),
	}})
	m.SetSize(29, 11)
	return m
}

func TestSidebarTitleShowsFocusMarkerWhenFocused(t *testing.T) {
	m := sidebarWithOneSession()
	m.SetFocused(true)
	if !strings.Contains(m.View(), "▸ SESSIONS") {
		t.Fatalf("focused sidebar title should contain the marker; got:\n%s", m.View())
	}
}

func TestSidebarTitleHasNoFocusMarkerWhenUnfocused(t *testing.T) {
	m := sidebarWithOneSession()
	m.SetFocused(false)
	v := m.View()
	if strings.Contains(v, "▸ SESSIONS") {
		t.Fatalf("unfocused sidebar title must not show the focus marker; got:\n%s", v)
	}
	if !strings.Contains(v, "SESSIONS") {
		t.Fatalf("unfocused sidebar should still show the SESSIONS title; got:\n%s", v)
	}
}

func TestSidebarWidthUnchangedByFocus(t *testing.T) {
	m := sidebarWithOneSession()
	m.SetFocused(true)
	focusedW := lipglossWidth(m.View())
	m.SetFocused(false)
	unfocusedW := lipglossWidth(m.View())
	if focusedW != unfocusedW {
		t.Fatalf("focus must not change rendered width: focused=%d unfocused=%d", focusedW, unfocusedW)
	}
}
```

Add a tiny helper at the bottom of the same file (kept local so the test is self-contained):

```go
func lipglossWidth(s string) int {
	w := 0
	for _, line := range strings.Split(s, "\n") {
		if lw := len([]rune(stripANSI(line))); lw > w {
			w = lw
		}
	}
	return w
}
```

If a `stripANSI` helper does not already exist in the `ui` package test files, use lipgloss directly instead — replace the helper body with:

```go
func lipglossWidth(s string) int {
	return lipgloss.Width(s)
}
```

and add `"github.com/charmbracelet/lipgloss"` to the imports. Prefer this lipgloss version.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestSidebar.*Focus -v`
Expected: FAIL — `SetFocused` undefined.

- [ ] **Step 3: Add the focused field and SetFocused**

In `internal/ui/sidebar.go`, add a field to `SidebarModel` (near the animation fields around line 71):

```go
	focused bool // whether the sidebar pane currently has keyboard focus
```

In `NewSidebar` (around line 76), initialize it to `true` (the list is focused at startup). Add to the returned struct literal:

```go
		focused: true,
```

Add the method near `SetBreathingFrame` (around line 153):

```go
// SetFocused marks whether the sidebar pane has keyboard focus. It re-renders on
// change so the lit title bar and content dimming update immediately.
func (m *SidebarModel) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.syncViewport()
}
```

- [ ] **Step 4: Render the lit/dim title bar in View**

In `internal/ui/sidebar.go`, replace the header construction in `View` (lines 669-677) with:

```go
	// Render the pane title: a lit reverse-video bar when focused, a dim title
	// otherwise. Both fill to m.width so focus never changes the pane width.
	activeCount := m.ActiveCount()
	countText := fmt.Sprintf("%d active", activeCount)
	var header string
	if m.focused {
		titleText := "▸ SESSIONS"
		gap := m.width - lipgloss.Width(titleText) - lipgloss.Width(countText) - 2
		if gap < 1 {
			gap = 1
		}
		inner := " " + titleText + strings.Repeat(" ", gap) + countText + " "
		header = styles.PaneTitleActive.Width(m.width).Render(inner)
	} else {
		title := styles.PaneTitleInactive.Render("SESSIONS")
		countStr := lipgloss.NewStyle().Foreground(styles.ColorDimText).Render(countText)
		gap := m.width - lipgloss.Width(title) - lipgloss.Width(countStr) - 2
		if gap < 1 {
			gap = 1
		}
		header = title + strings.Repeat(" ", gap) + countStr + " "
	}
```

The two later `return lipgloss.JoinVertical(lipgloss.Left, header, ...)` lines (696, 701) stay as-is.

- [ ] **Step 5: Dim the non-selected group headers**

In `renderGroupHeader`, in the normal (non-cursor) branch (lines 745-752), replace with:

```go
	// Normal group header: triangle + name left, count right-aligned. Dim toward
	// DimText when the pane is unfocused.
	ghStyle := styles.SidebarGroupHeader
	gcStyle := styles.SidebarGroupCount
	if !m.focused {
		ghStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText).PaddingLeft(1)
		gcStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText)
	}
	left := ghStyle.Render(fmt.Sprintf("%s %s", arrow, name))
	right := gcStyle.Render(fmt.Sprintf("%d", count))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
```

(The `if idx > 0 { return "\n" + line }` tail stays.)

- [ ] **Step 6: Dim the normal (non-cursor) session rows, keep the status dot colored**

In `renderSessionItem`, in the normal-item branch (lines 879-893), replace with:

```go
	// Normal item. The status icon keeps its semantic color even when dimmed so
	// session status stays glanceable across panes; only the text dims.
	icon := statusIcon(s.Status, m.breathingFrame)
	nameStyle := styles.SidebarProjectName
	timeStyle := styles.SidebarTime
	summaryStyle := styles.SidebarSummary
	if !m.focused {
		nameStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText)
		timeStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText)
		summaryStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText).PaddingLeft(4)
	}
	nameStyled := nameStyle.Render(displayName)
	timeStyled := timeStyle.Render(relTime)

	iconWidth := lipgloss.Width(icon)
	nameWidth := lipgloss.Width(nameStyled)
	timeWidth := lipgloss.Width(timeStyled)
	// 2 spaces indent + icon + 1 space + name + gap + time + 1 right pad
	gap := m.width - 2 - iconWidth - 1 - nameWidth - timeWidth - 1
	if gap < 1 {
		gap = 1
	}
	line1 := "  " + icon + " " + nameStyled + strings.Repeat(" ", gap) + timeStyled + " "
	line2 := summaryStyle.Render(summary)

	return []string{line1, line2}
```

The selected/cursor branch (lines 787-877) is intentionally left unchanged: the cursor row stays bright even when the pane is unfocused so its position remains visible. (A muted-selection variant is a possible later refinement.)

- [ ] **Step 7: Run the sidebar focus tests**

Run: `go test ./internal/ui/ -run TestSidebar -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/sidebar.go internal/ui/sidebar_focus_test.go
git commit -m "feat(ui): lit title + dimmed content for sidebar focus state"
```

---

### Task 3: Changed-files focus rendering (lit title, dim rows/stats)

**Files:**
- Modify: `internal/ui/changedfiles.go` (struct lines 19-26; `NewChangedFiles` lines 29-36; new `SetFocused`; `renderRow` lines 152-179; `renderStats` lines 185-200; `View` lines 215-222)
- Test: `internal/ui/changedfiles_focus_test.go` (new)

> NOTE (re-synced 2026-07-22): a later commit (`5d8eff1`) rewrote `renderRow` to truncate the path from the LEFT via `truncateDisplayLeft` (so the +/- counts are never cut off) and uses `inner := m.width - 2`. The Step 4 code below is written against that CURRENT `renderRow`, not the original. `SetFiles` now also preserves cursor position — unrelated to focus, leave it alone.

**Interfaces:**
- Consumes: `styles.PaneTitleActive`, `styles.PaneTitleInactive`, `styles.ColorDimText`.
- Produces: `(*ChangedFilesModel).SetFocused(bool)`; changed-files renders `▸ CHANGED FILES` lit bar when focused, dims rows and +/- stats when not.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/changedfiles_focus_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func changedFilesWithOne() ChangedFilesModel {
	m := NewChangedFiles(30, 12)
	m.SetFiles([]session.ChangedFile{
		{Path: "/repo/internal/app/app.go", Added: 3, Removed: 1},
	}, "/repo")
	m.SetSize(29, 11)
	return m
}

func TestChangedFilesTitleShowsFocusMarkerWhenFocused(t *testing.T) {
	m := changedFilesWithOne()
	m.SetFocused(true)
	if !strings.Contains(m.View(), "▸ CHANGED FILES") {
		t.Fatalf("focused changed-files title should contain the marker; got:\n%s", m.View())
	}
}

func TestChangedFilesTitleHasNoMarkerWhenUnfocused(t *testing.T) {
	m := changedFilesWithOne()
	m.SetFocused(false)
	v := m.View()
	if strings.Contains(v, "▸ CHANGED FILES") {
		t.Fatalf("unfocused changed-files title must not show the marker; got:\n%s", v)
	}
	if !strings.Contains(v, "CHANGED FILES") {
		t.Fatalf("unfocused changed-files should still show its title; got:\n%s", v)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestChangedFiles.*Focus -v`
Expected: FAIL — `SetFocused` undefined.

- [ ] **Step 3: Add the focused field, init, and SetFocused**

In `internal/ui/changedfiles.go`, add to `ChangedFilesModel` (after `vp` around line 25):

```go
	focused bool // whether the changed-files pane currently has keyboard focus
```

`NewChangedFiles` leaves it at the zero value `false` (the panel is unfocused until opened) — no change needed to the struct literal.

Add the method after `Reset` (around line 78):

```go
// SetFocused marks whether the changed-files pane has keyboard focus, re-rendering
// on change so the lit title and row dimming update immediately.
func (m *ChangedFilesModel) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.syncViewport()
}
```

- [ ] **Step 4: Dim rows when unfocused; thread focus into stats**

Replace the CURRENT `renderRow` (lines 152-179) with the version below. It keeps the existing left-truncation logic (`truncateDisplayLeft`, `inner := m.width - 2`) exactly and only adds the focus-dimming branch and the dim flag to `renderStats`:

```go
func (m ChangedFilesModel) renderRow(f session.ChangedFile, selected bool) string {
	stats := m.renderStats(f, !m.focused)
	statsWidth := lipgloss.Width(stats)

	// Content occupies m.width minus the two columns the row style adds (left
	// padding, or the selection bar + padding on the selected row).
	inner := m.width - 2
	if inner < 1 {
		inner = 1
	}

	nameWidth := inner - statsWidth - 1 // -1 for the gap before the counts
	var name string
	if nameWidth >= 1 {
		name = truncateDisplayLeft(m.relPath(f.Path), nameWidth)
	}

	gap := inner - lipgloss.Width(name) - statsWidth
	if gap < 1 {
		gap = 1
	}
	content := name + strings.Repeat(" ", gap) + stats

	if selected {
		// Cursor row stays bright even when unfocused so its position is visible.
		return styles.SidebarItemSelected.Render(content)
	}
	if !m.focused {
		// SidebarItem has PaddingLeft(2); match it so width is unchanged.
		return lipgloss.NewStyle().Foreground(styles.ColorDimText).PaddingLeft(2).Render(content)
	}
	return styles.SidebarItem.Render(content)
}
```

Replace `renderStats` (lines 185-200) signature and body with:

```go
// renderStats renders "+A" in green and "-R" in red, omitting a zero side. When
// dim is true (pane unfocused) both sides render in DimText instead. Estimated
// counts (no live git diff) render faintly. Styles are built per render from the
// active theme's colors so they follow theme switches.
func (m ChangedFilesModel) renderStats(f session.ChangedFile, dim bool) string {
	addColor, remColor := styles.ColorGreen, styles.ColorRed
	if dim {
		addColor, remColor = styles.ColorDimText, styles.ColorDimText
	}
	added := lipgloss.NewStyle().Foreground(addColor)
	removed := lipgloss.NewStyle().Foreground(remColor)
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
```

- [ ] **Step 5: Render the lit/dim title bar in View**

Replace the header construction in `View` (lines 215-222) with:

```go
	countText := fmt.Sprintf("%d files", len(m.files))
	var header string
	if m.focused {
		titleText := "▸ CHANGED FILES"
		gap := m.width - lipgloss.Width(titleText) - lipgloss.Width(countText) - 2
		if gap < 1 {
			gap = 1
		}
		inner := " " + titleText + strings.Repeat(" ", gap) + countText + " "
		header = styles.PaneTitleActive.Width(m.width).Render(inner)
	} else {
		title := styles.PaneTitleInactive.Render("CHANGED FILES")
		countStr := lipgloss.NewStyle().Foreground(styles.ColorDimText).Render(countText)
		gap := m.width - lipgloss.Width(title) - lipgloss.Width(countStr) - 2
		if gap < 1 {
			gap = 1
		}
		header = title + strings.Repeat(" ", gap) + countStr + " "
	}
```

The two later `return lipgloss.JoinVertical(...)` lines (~227, ~230) stay as-is.

- [ ] **Step 6: Run the changed-files focus tests plus the existing suite**

Run: `go test ./internal/ui/ -run TestChangedFiles -v`
Expected: PASS (new focus tests and existing changedfiles tests).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/changedfiles.go internal/ui/changedfiles_focus_test.go
git commit -m "feat(ui): lit title + dimmed rows for changed-files focus state"
```

---

### Task 4: Preview lit focus chip

**Files:**
- Modify: `internal/ui/preview.go` (`renderHeader` line 212)
- Test: `internal/ui/preview_focus_test.go` (new)

**Interfaces:**
- Consumes: `styles.PaneTitleActive`; existing `m.passthrough` bool (set via `SetPassthrough`). The thick blue header underline comes from `styles.PreviewHeaderFocused` (already applied when `m.passthrough`, thickened in Task 1).
- Produces: preview header shows a lit `▸ <project>` chip when the preview is focused (passthrough), plain blue name otherwise.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/preview_focus_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func previewWithSession() PreviewModel {
	m := NewPreview(60, 12)
	m.SetSession(&session.Session{
		ProjectName: "myproj",
		TmuxTarget:  "work:1.0",
		Status:      session.StatusActive,
	})
	return m
}

func TestPreviewShowsFocusChipWhenPassthrough(t *testing.T) {
	m := previewWithSession()
	m.SetPassthrough(true)
	if !strings.Contains(m.View(), "▸ myproj") {
		t.Fatalf("focused preview header should contain the lit chip; got:\n%s", m.View())
	}
}

func TestPreviewHasNoFocusChipWhenNotPassthrough(t *testing.T) {
	m := previewWithSession()
	m.SetPassthrough(false)
	v := m.View()
	if strings.Contains(v, "▸ myproj") {
		t.Fatalf("unfocused preview header must not show the focus chip; got:\n%s", v)
	}
	if !strings.Contains(v, "myproj") {
		t.Fatalf("preview header should still show the project name; got:\n%s", v)
	}
}
```

Note: confirm `NewPreview`, `SetSession`, `SetPassthrough` signatures match preview.go before running; adjust the constructor call if the real signature differs (check `internal/ui/preview.go` top and `internal/ui/preview_test.go` for the exact constructor usage).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestPreview.*Chip -v`
Expected: FAIL — header has no `▸ myproj` chip yet.

- [ ] **Step 3: Render the lit chip in renderHeader**

In `internal/ui/preview.go`, replace the project-name append (line 212) with:

```go
	// Project name: a lit reverse-video chip when the preview is focused
	// (passthrough), plain blue bold otherwise.
	if m.passthrough {
		leftParts = append(leftParts, styles.PaneTitleActive.Render(" ▸ "+projectName+" "))
	} else {
		leftParts = append(leftParts, lipgloss.NewStyle().Foreground(styles.ColorBlue).Bold(true).Render(projectName))
	}
```

- [ ] **Step 4: Run the preview focus tests plus the existing preview suite**

Run: `go test ./internal/ui/ -run TestPreview -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_focus_test.go
git commit -m "feat(ui): lit focus chip in preview header on passthrough"
```

---

### Task 5: Wire focus into the app layout (focusedPane + View)

**Files:**
- Create: `internal/app/focus.go`
- Modify: `internal/app/app.go` `View` (separator selection lines 713-747; add `SetFocused` calls before rendering the panes)
- Test: `internal/app/focus_test.go` (new)

**Interfaces:**
- Consumes: `(*ui.SidebarModel).SetFocused`, `(*ui.ChangedFilesModel).SetFocused` (Tasks 2, 3); `styles.SidebarPanelFocused`, `styles.RightSidebarPanelFocused` (Task 1).
- Produces: `Model.focusedPane() Pane`; `Pane` enum (`PaneList`, `PanePreview`, `PaneFiles`). The sidebar's right edge is thick-blue only when List is focused; the changed-files' left edge is thick-blue only when Files is focused; both panes are told their focus state each frame.

- [ ] **Step 1: Write the failing test**

Create `internal/app/focus_test.go`:

```go
package app

import "testing"

func TestFocusedPaneMapping(t *testing.T) {
	cases := map[Mode]Pane{
		ModeList:          PaneList,
		ModeSearch:        PaneList,
		ModeNameInput:     PaneList,
		ModeRenameSession: PaneList,
		ModeContextMenu:   PaneList,
		ModeHelp:          PaneList,
		ModeDetail:        PaneList,
		ModeStats:         PaneList,
		ModeThemePicker:   PaneList,
		ModeDirPicker:     PaneList,
		ModeResumePicker:  PaneList,
		ModePassthrough:   PanePreview,
		ModeChangedFiles:  PaneFiles,
	}
	for mode, want := range cases {
		m := Model{mode: mode}
		if got := m.focusedPane(); got != want {
			t.Errorf("mode %v: focusedPane() = %v, want %v", mode, got, want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/app/ -run TestFocusedPane -v`
Expected: FAIL — `Pane`, `PaneList`, `focusedPane` undefined.

- [ ] **Step 3: Add the Pane type and focusedPane helper**

Create `internal/app/focus.go`:

```go
package app

// Pane identifies one of the three focusable layout panes.
type Pane int

const (
	// PaneList is the left session-list sidebar.
	PaneList Pane = iota
	// PanePreview is the center preview panel.
	PanePreview
	// PaneFiles is the right changed-files sidebar.
	PaneFiles
)

// focusedPane maps the current input mode to the pane that owns keyboard focus.
// Passthrough focuses the preview; the changed-files mode focuses the right
// sidebar; every other mode (list, search, inline inputs, and modal overlays
// that sit above the list) keeps focus on the session list.
func (m Model) focusedPane() Pane {
	switch m.mode {
	case ModePassthrough:
		return PanePreview
	case ModeChangedFiles:
		return PaneFiles
	default:
		return PaneList
	}
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/app/ -run TestFocusedPane -v`
Expected: PASS.

- [ ] **Step 5a: Tell the panes their focus state BEFORE they render**

Ordering matters: `sidebarView` is built by calling `m.sidebar.View()` inside the sidebar block (lines 677-708), which is *earlier* than the sidebar style block. The sidebar's title bar reads `m.focused` at `View()` time and its rows are laid out by `syncViewport`, so focus must be set before that block runs. `SetFocused` re-renders only on change, and it operates on the per-frame `Model` copy (View has a value receiver), so it is cheap and self-correcting.

In `internal/app/app.go` `View`, immediately after `m.sidebar.SetSpinnerView(m.spinner.View())` (line 673) and before the sidebar-building block, insert:

```go
	// Focus drives all three pane signals. Tell the sidebar and changed-files
	// panes their focus state (they re-render only when it changes) before they
	// render below. The preview owns no edge, so preview-focus shows via its
	// header chip + the neighbors dimming, handled in preview.go / their dim paths.
	fp := m.focusedPane()
	m.sidebar.SetFocused(fp == PaneList)
	m.rightSidebar.SetFocused(fp == PaneFiles)
```

- [ ] **Step 5b: Light the active pane's separator edge**

Replace the sidebar style block (lines 713-722) — which currently reads `sidebarStyle := styles.SidebarPanel; if m.mode == ModePassthrough { sidebarStyle = styles.SidebarPanelFocused }` — with a version keyed off `fp` (the old passthrough trigger is removed; the sidebar edge now lights for List focus):

```go
	sidebarStyle := styles.SidebarPanel
	if fp == PaneList {
		sidebarStyle = styles.SidebarPanelFocused
	}
	sidebarView = sidebarStyle.
		Width(sidebarWidth - 1). // -1 for the border character
		Height(contentHeight).
		MaxHeight(contentHeight).
		Render(sidebarView)
```

Then in the changed-files block (lines 735-747), replace the style selection so it keys off `fp`:

```go
	if rightWidth > 0 {
		m.rightSidebar.SetSize(rightWidth-1, contentHeight)
		rightStyle := styles.RightSidebarPanel
		if fp == PaneFiles {
			rightStyle = styles.RightSidebarPanelFocused
		}
		rightView := rightStyle.
			Width(rightWidth - 1). // -1 for the border character
			Height(contentHeight).
			MaxHeight(contentHeight).
			Render(m.rightSidebar.View())
		columns = append(columns, rightView)
	}
```

Note the removed old logic: `if m.mode == ModePassthrough { sidebarStyle = styles.SidebarPanelFocused }` is gone — the sidebar edge now lights for List focus, not passthrough. The `m.sidebar.SetSize(...)` calls earlier in View (lines 685/694/703/706) already run before this block; `SetFocused` here re-renders the sidebar's viewport when focus changes, so ordering is fine (SetFocused's syncViewport uses the size already set).

- [ ] **Step 6: Build and run the full app test suite**

Run: `go build ./... && go test ./internal/app/ ./internal/ui/ ./internal/styles/ -v 2>&1 | tail -40`
Expected: build succeeds; all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/focus.go internal/app/app.go internal/app/focus_test.go
git commit -m "feat(app): drive pane focus signals from focusedPane in View"
```

---

### Task 6: Full verification and visual smoke test

**Files:** none (verification only)

- [ ] **Step 1: Full build, vet, and test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: no build/vet errors; all packages PASS.

- [ ] **Step 2: Confirm no width regression across the whole binary**

Run: `go test ./internal/ui/ -run 'Width' -v`
Expected: PASS (`TestSidebarWidthUnchangedByFocus`).

- [ ] **Step 3: Visual smoke test (manual, requires a tmux server)**

Build and run the binary inside tmux; verify by eye:

```bash
go build -o naviclaude ./cmd/naviclaude
```

Then, in a tmux session, run `./naviclaude` and check:
- List focus (default): `▸ SESSIONS` is a solid blue bar; sidebar right edge is thick blue; preview + changed-files titles are dim and their content dimmed.
- Press Tab / Enter into a session (passthrough): preview header shows `▸ <project>` lit chip + thick blue underline; sidebar title/edge go dim and the list content dims (status dots stay colored).
- Open changed-files (its toggle key) and Tab to it: `▸ CHANGED FILES` bar lit, its left edge thick blue; other panes dim.
- Switch theme (theme picker) to at least one light theme (catppuccin-latte) and one other dark theme; confirm the lit bar text stays readable and the active edge is clearly the accent color in each.

- [ ] **Step 4: Final commit (only if any tweak was needed)**

```bash
git add -A
git commit -m "chore: verify active-pane focus across themes"
```

---

## Self-Review

**Spec coverage:**
- One focus concept (`focusedPane`) — Task 5.
- Lit title bar (sidebar/files/preview chip) — Tasks 2, 3, 4 (+ styles in Task 1).
- Active separator edge, thick + blue, owned per pane, no width cost — Tasks 1 (thick styles) + 5 (per-pane selection); width guard test in Task 2/6.
- Dimmed inactive content, sidebar keeps status dots — Task 2 (dots via `statusIcon`, unchanged, semantic) + Task 3 (files dim).
- Preview body not dimmed — honored (only chip + underline change in Task 4).
- All 11 palettes, no hardcoded color — Task 1 styles derive from palette; `focus_contrast_test.go` guards `Blue != Border` and `Blue != Bg`.

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every test step shows the assertion and the run command with expected result.

**Type consistency:** `SetFocused(bool)` identical on both `*SidebarModel` and `*ChangedFilesModel`; `focusedPane() Pane` and `Pane`/`PaneList`/`PanePreview`/`PaneFiles` used consistently across Tasks 5 and its test; `renderStats(f, dim bool)` signature changed and its one call site in `renderRow` updated in the same task (Task 3); `PaneTitleActive`/`PaneTitleInactive` defined in Task 1, consumed in Tasks 2/3/4.

**Deliberate v1 scope:** the selected/cursor row stays bright in an unfocused pane (cursor visibility); a muted-selection variant is a possible follow-up, not part of this plan.
