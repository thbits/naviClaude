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
