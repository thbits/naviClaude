package app

import "testing"

func TestFocusedPaneMapping(t *testing.T) {
	// This map must list every Mode. focusedPane() has a `default -> PaneList`
	// fallthrough, so a forgotten new mode would silently resolve to PaneList
	// here too instead of failing -- there is no "missing case" signal to catch it.
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
