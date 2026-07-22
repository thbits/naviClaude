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
