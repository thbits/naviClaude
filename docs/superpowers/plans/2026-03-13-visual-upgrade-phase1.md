# Visual Upgrade Phase 1: Style Polish -- Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make naviClaude's sidebar, title bar, and status bar visually refined with gradient accents, letter-spaced header, dot separators, and better selection styling.

**Architecture:** Pure style changes -- modify `styles.go`, `themes.go` for new color/style definitions, then update render functions in `sidebar.go`, `statusbar.go`, `preview.go`, and `app.go` to use them. No new state, no new dependencies, no new tick subscriptions.

**Tech Stack:** Go, Lipgloss v1.1.0, Bubble Tea v1.3.10

---

## Chunk 1: Theme & Style Foundation

### Task 1: Add SelectionDim to Palette and all themes

**Files:**
- Modify: `internal/styles/themes.go:11-28` (Palette struct)
- Modify: `internal/styles/themes.go:31-212` (all 10 theme definitions)
- Modify: `internal/styles/styles.go:16` (add ColorSelectionDim variable)

- [ ] **Step 1: Add SelectionDim field to Palette struct**

In `internal/styles/themes.go`, add `SelectionDim` after `Selection` (line 17):

```go
type Palette struct {
	Name         string
	Bg           lipgloss.Color // terminal background
	BgPanel      lipgloss.Color // sidebar/statusbar panel bg
	BgHover      lipgloss.Color // hover/key badge bg
	Fg           lipgloss.Color // primary foreground
	Selection    lipgloss.Color // selected item background
	SelectionDim lipgloss.Color // dimmer selection variant for depth effect
	Blue         lipgloss.Color // primary accent
	Green        lipgloss.Color // active, success
	Amber        lipgloss.Color // waiting, warning
	Red          lipgloss.Color // danger, kill
	Gray         lipgloss.Color // secondary text
	Purple       lipgloss.Color // model, secondary accent
	Cyan         lipgloss.Color // values, highlights
	Border       lipgloss.Color // borders, separators
	Dim          lipgloss.Color // faint elements
	DimText      lipgloss.Color // closed session text
}
```

- [ ] **Step 2: Add SelectionDim to all 10 theme definitions**

Add `SelectionDim` to each theme. The value should be ~15% darker than `Selection` for dark themes, ~15% lighter for Catppuccin Latte. Values for each theme:

```go
// tokyo-night: Selection "#2a3a5e"
SelectionDim: lipgloss.Color("#243350"),

// catppuccin-mocha: Selection "#313244"
SelectionDim: lipgloss.Color("#2a2b3c"),

// catppuccin-latte: Selection "#dce0e8" (light theme -- dim is lighter/more washed)
SelectionDim: lipgloss.Color("#e4e7ee"),

// dracula: Selection "#44475a"
SelectionDim: lipgloss.Color("#3b3e50"),

// nord: Selection "#3b4252"
SelectionDim: lipgloss.Color("#333a49"),

// one-dark: Selection "#3e4451"
SelectionDim: lipgloss.Color("#363b47"),

// gruvbox: Selection "#504945"
SelectionDim: lipgloss.Color("#45403c"),

// solarized-dark: Selection "#073642"
SelectionDim: lipgloss.Color("#062e38"),

// rose-pine: Selection "#26233a"
SelectionDim: lipgloss.Color("#201e32"),

// kanagawa: Selection "#2d4f67"
SelectionDim: lipgloss.Color("#26445b"),
```

- [ ] **Step 3: Add ColorSelectionDim variable to styles.go**

In `internal/styles/styles.go`, add after line 16 (`ColorSelection`):

```go
ColorSelectionDim = lipgloss.Color("#243350") // dimmer selection for depth effect
```

- [ ] **Step 4: Wire SelectionDim into ApplyTheme**

In `internal/styles/styles.go` `ApplyTheme()` function, add after the `ColorSelection = p.Selection` line:

```go
ColorSelectionDim = p.SelectionDim
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./...`
Expected: Clean build, no errors.

- [ ] **Step 6: Run tests**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go test ./...`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/styles/themes.go internal/styles/styles.go
git commit -m "feat(styles): add SelectionDim color to palette and all themes"
```

---

### Task 2: Update dot separators in styles

**Files:**
- Modify: `internal/ui/statusbar.go:89`
- Modify: `internal/ui/preview.go:141`
- Modify: `internal/app/app.go:1420`

- [ ] **Step 1: Change status bar separator**

In `internal/ui/statusbar.go` line 89, change:
```go
sep := styles.StatusBarSep.Render(" | ")
```
to:
```go
sep := styles.StatusBarSep.Render(" \u2022 ")
```

- [ ] **Step 2: Change preview header separator**

In `internal/ui/preview.go` line 141, change:
```go
sep := styles.PreviewSep.Render(" | ")
```
to:
```go
sep := styles.PreviewSep.Render(" \u2022 ")
```

- [ ] **Step 3: Change title bar separator**

In `internal/app/app.go` line 1420, change:
```go
sep := styles.TitleBarDim.Render(" | ")
```
to:
```go
sep := styles.TitleBarDim.Render(" \u2022 ")
```

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./... && go test ./...`
Expected: Clean build, all tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/ui/statusbar.go internal/ui/preview.go internal/app/app.go
git commit -m "feat(ui): replace pipe separators with centered dot for softer visual rhythm"
```

---

## Chunk 2: Title Bar & Sidebar Header Polish

### Task 3: Gradient title text

**Files:**
- Modify: `internal/app/app.go:1417-1438` (renderTitleBar method)

- [ ] **Step 1: Add gradientText helper function**

Add this function near `renderTitleBar()` in `internal/app/app.go`:

```go
// gradientText renders an ASCII string with alternating blue/purple characters
// to create a character-level gradient effect. Uses byte offset for index,
// which equals rune index for ASCII-only input like "naviClaude".
func gradientText(s string) string {
	var b strings.Builder
	for i, ch := range s {
		color := styles.ColorBlue
		if i%2 == 1 {
			color = styles.ColorPurple
		}
		b.WriteString(lipgloss.NewStyle().Foreground(color).Bold(true).Render(string(ch)))
	}
	return b.String()
}
```

- [ ] **Step 2: Update renderTitleBar to use gradientText**

In `internal/app/app.go`, change line 1418 from:
```go
left := styles.TitleBarName.Render(" naviClaude")
```
to:
```go
left := " " + gradientText("naviClaude")
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./... && go test ./...`
Expected: Clean build, all tests pass.

- [ ] **Step 4: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/app/app.go
git commit -m "feat(ui): gradient blue-purple title text for naviClaude brand"
```

---

### Task 4: Letter-spaced SESSIONS header

**Files:**
- Modify: `internal/ui/sidebar.go:527-557` (View method)

- [ ] **Step 1: Add letterSpace helper**

Add this helper near the top of `internal/ui/sidebar.go` (after the imports):

```go
// letterSpace inserts a space between each character of s.
func letterSpace(s string) string {
	runes := []rune(s)
	if len(runes) <= 1 {
		return s
	}
	var b strings.Builder
	for i, r := range runes {
		if i > 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
```

- [ ] **Step 2: Use letterSpace in the View() header rendering**

In `internal/ui/sidebar.go` line 530, change:
```go
title := styles.SidebarTitle.Render("SESSIONS")
```
to:
```go
title := styles.SidebarTitle.Render(letterSpace("SESSIONS"))
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./... && go test ./...`
Expected: Clean build, all tests pass. Note: the letter-spaced title is wider (15 chars vs 8), so the gap calculation on line 532 will naturally adjust.

- [ ] **Step 4: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/ui/sidebar.go
git commit -m "feat(ui): letter-spaced SESSIONS header for cleaner typography"
```

---

### Task 4b: Improved item spacing

**Files:**
- Modify: `internal/ui/sidebar.go:231-246` (rebuildFlatItems method)
- Modify: `internal/ui/sidebar.go:565-603` (renderGroupHeader method)

- [ ] **Step 1: Add vertical gap between groups in renderGroupHeader**

In `internal/ui/sidebar.go`, modify `renderGroupHeader()` to prepend an empty line before each group header except the first one. Find the function (line 565). Add logic at the top:

After the arrow/collapsed check (around line 566-569), before the count lookup, determine if this is the first group:

The simplest approach: add a blank line as a return prefix for non-first groups. Modify the end of the function. Change the normal (non-cursor) return (around line 596-602) from:

```go
	left := styles.SidebarGroupHeader.Render(fmt.Sprintf("%s %s", arrow, name))
	right := styles.SidebarGroupCount.Render(fmt.Sprintf("%d", count))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
```

to:

```go
	left := styles.SidebarGroupHeader.Render(fmt.Sprintf("%s %s", arrow, name))
	right := styles.SidebarGroupCount.Render(fmt.Sprintf("%d", count))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	// Add vertical breathing room before non-first groups.
	if m.groupIndex(name) > 0 {
		return "\n" + line
	}
	return line
```

And similarly for the cursor branch (around line 580-592), wrap the return:

Change:
```go
		return styles.SidebarItemSelected.Width(m.width - 1).Render(full)
```
to:
```go
		rendered := styles.SidebarItemSelected.Width(m.width - 1).Render(full)
		if m.groupIndex(name) > 0 {
			return "\n" + rendered
		}
		return rendered
```

- [ ] **Step 2: Add groupIndex helper method**

Add this method to SidebarModel in `sidebar.go`:

```go
// groupIndex returns the position of the named group (0-based), or -1 if not found.
func (m SidebarModel) groupIndex(name string) int {
	for i, g := range m.groups {
		if g.Name == name {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./... && go test ./...`
Expected: Clean build, all tests pass.

- [ ] **Step 4: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/ui/sidebar.go
git commit -m "feat(ui): add vertical breathing room between sidebar groups"
```

---

## Chunk 3: Selection Bar & Background Refinement

### Task 5: Gradient selection left bar

**Files:**
- Modify: `internal/ui/sidebar.go:605-715` (renderSessionItem method)

The current selection uses `SelectionIndicator` (a `\u258e` left quarter-block border in blue). We'll replace this with a custom-rendered 2-char gradient column: blue quarter-block on line 1, purple quarter-block on line 2 (the summary line).

- [ ] **Step 1: Modify selected session rendering for gradient bar**

In `internal/ui/sidebar.go`, in the `renderSessionItem` method, find the selected item rendering block (the `if isCursor` branch starting around line 636). Replace the line1Style and line2Style definitions.

Change the line1 border color (around line 666-673):
```go
		borderFg := styles.ColorBlue
		if isConfirmingKill {
			borderFg = styles.ColorRed
		}
		line1Style := lipgloss.NewStyle().
			Foreground(styles.ColorBlue).
			Background(selBg).
			Bold(true).
			PaddingLeft(1).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(borderFg)
```

Keep this as-is for line 1 (blue bar), but change line2Style (around line 687-693) to use purple and SelectionDim background:

Change:
```go
		line2Style := lipgloss.NewStyle().
			Foreground(styles.ColorGray).
			Background(selBg).
			PaddingLeft(3).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(borderFg)
```
to:
```go
		line2BorderFg := styles.ColorPurple
		if isConfirmingKill {
			line2BorderFg = styles.ColorRed
		}
		line2Style := lipgloss.NewStyle().
			Foreground(styles.ColorGray).
			Background(styles.ColorSelectionDim).
			PaddingLeft(3).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(line2BorderFg)
```

- [ ] **Step 2: Update summary line background color references**

Also in the selected session rendering, update the line2Content section. When NOT confirming kill, the summary text should use `ColorSelectionDim` background. Find the summary rendering (around line 684):

```go
		} else {
			line2Content = summary
		}
```

This is fine as-is since the line2Style already applies the background.

But we also need to update the kill confirmation line to use `ColorSelectionDim`:

Change:
```go
		if isConfirmingKill {
			killLabel := lipgloss.NewStyle().Foreground(styles.ColorRed).Background(selBg).Bold(true).Render("Kill?")
			yKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(selBg).Render(" y")
			slash := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(selBg).Render("/")
			nKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(selBg).Bold(true).Render("N")
```
to:
```go
		if isConfirmingKill {
			dimBg := styles.ColorSelectionDim
			killLabel := lipgloss.NewStyle().Foreground(styles.ColorRed).Background(dimBg).Bold(true).Render("Kill?")
			yKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(dimBg).Render(" y")
			slash := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(dimBg).Render("/")
			nKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(dimBg).Bold(true).Render("N")
```

- [ ] **Step 3: Update selected group header summary styling**

In `renderGroupHeader` (around line 565), the selected group header also uses `SidebarItemSelected`. This is fine -- group headers are single-line, so no gradient effect needed. Leave as-is.

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./... && go test ./...`
Expected: Clean build, all tests pass.

- [ ] **Step 5: Build and run manually to verify visual**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build -o naviclaude ./cmd/naviclaude`
Then launch `./naviclaude` and verify:
- Selected item line 1 has blue left bar
- Selected item line 2 (summary) has purple left bar and slightly dimmer background
- Kill confirmation also shows the dimmer background
- All themes render correctly (try `t` to open theme picker)

- [ ] **Step 6: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/ui/sidebar.go
git commit -m "feat(ui): gradient selection bar (blue->purple) with dim summary background"
```

---

### Task 6: Update SidebarSummarySelected style

**Files:**
- Modify: `internal/styles/styles.go` (SidebarSummarySelected definition and ApplyTheme)

The `SidebarSummarySelected` style is defined but may be used elsewhere. Update it to use `ColorSelectionDim` for consistency.

- [ ] **Step 1: Update SidebarSummarySelected initial definition**

In `internal/styles/styles.go`, change the `SidebarSummarySelected` definition (around line 107-113):

```go
var SidebarSummarySelected = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorSelectionDim).
	PaddingLeft(3).
	BorderLeft(true).
	BorderStyle(SelectionIndicator).
	BorderForeground(ColorPurple)
```

- [ ] **Step 2: Update SidebarSummarySelected in ApplyTheme**

In `internal/styles/styles.go` `ApplyTheme()`, find the `SidebarSummarySelected` rebuild (around line 507-512) and change to:

```go
	SidebarSummarySelected = lipgloss.NewStyle().
		Foreground(ColorGray).
		Background(ColorSelectionDim).
		PaddingLeft(3).
		BorderLeft(true).
		BorderStyle(SelectionIndicator).
		BorderForeground(ColorPurple)
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build ./... && go test ./...`
Expected: Clean build, all tests pass.

- [ ] **Step 4: Commit**

```bash
cd /Users/tomhalo/personal/git/naviClaude
git add internal/styles/styles.go
git commit -m "feat(styles): SidebarSummarySelected uses SelectionDim bg and purple bar"
```

---

## Final Verification

- [ ] **Full build and test**

Run: `cd /Users/tomhalo/personal/git/naviClaude && go build -o naviclaude ./cmd/naviclaude && go test ./...`

- [ ] **Manual visual verification checklist**

Launch `./naviclaude` and check:
1. Title bar: "naviClaude" shows alternating blue/purple characters
2. Title bar: dot separator between name and session count
3. SESSIONS header: letter-spaced "S E S S I O N S"
4. Selected item line 1: blue left bar, blue text, selection background
5. Selected item line 2: purple left bar, dimmer background
6. Status bar: dot separators between key hints
7. Preview header: dot separators between metadata
8. All 10 themes: open theme picker (t), cycle through each, verify all colors work
9. Kill confirmation (K on a session): dimmer background on confirmation line
