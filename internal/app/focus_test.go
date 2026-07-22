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
