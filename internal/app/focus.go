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

// focusedPane maps the input mode to the pane owning keyboard focus; every mode
// but passthrough and changed-files, overlays included, keeps it on the list.
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
