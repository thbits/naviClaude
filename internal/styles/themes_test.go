package styles

import "testing"

func TestKeyResolvesKeysAndDisplayNames(t *testing.T) {
	for name, want := range map[string]string{
		"catppuccin-latte":  "catppuccin-latte",
		"Catppuccin Latte":  "catppuccin-latte",
		"catppuccin latte":  "catppuccin-latte",
		" Rose Pine Dawn ":  "rose-pine-dawn",
		"TOKYO-NIGHT":       "tokyo-night",
		"Gruvbox Dark":      "gruvbox",
		"no-such-theme":     fallbackThemeKey,
		"":                  fallbackThemeKey,
		"Catppuccin  Latte": fallbackThemeKey, // not a spelling of the name
	} {
		if got := Key(name); got != want {
			t.Errorf("Key(%q) = %q, want %q", name, got, want)
		}
	}
	if Named("Catppuccin Latte").Name != "Catppuccin Latte" {
		t.Error("Named should accept a display name")
	}
}

// Auto light/dark needs light themes to switch to; each must be one of Themes.
func TestLightThemesExist(t *testing.T) {
	for _, key := range []string{"catppuccin-latte", "rose-pine-dawn"} {
		p, ok := Themes[key]
		if !ok {
			t.Errorf("light theme %q is missing", key)
			continue
		}
		if !lighter(string(p.Bg), string(p.Fg)) {
			t.Errorf("theme %q: background %s is not lighter than foreground %s", key, p.Bg, p.Fg)
		}
	}
}

// A preview search paints matches on Amber and the current one on Fg, with Bg
// text on both: equal colors would hide the text or the current match.
func TestSearchMatchColorsContrastInAllThemes(t *testing.T) {
	for name, p := range Themes {
		if p.Amber == p.Fg {
			t.Errorf("theme %q: Amber == Fg (%s); the current match would look like the others", name, p.Fg)
		}
		if p.Bg == p.Amber || p.Bg == p.Fg {
			t.Errorf("theme %q: match text (Bg %s) would vanish on its highlight", name, p.Bg)
		}
	}
}

// lighter compares two "#rrggbb" colors by the sum of their channels.
func lighter(a, b string) bool {
	sum := func(hex string) int {
		n := 0
		for i := 1; i+1 < len(hex); i += 2 {
			n += hexByte(hex[i])*16 + hexByte(hex[i+1])
		}
		return n
	}
	return sum(a) > sum(b)
}

func hexByte(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}
