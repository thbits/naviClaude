package styles

import (
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// Palette defines the semantic color roles for a theme.
// All colors are lipgloss hex color strings.
type Palette struct {
	Name      string
	Bg        lipgloss.Color // terminal background
	BgPanel   lipgloss.Color // sidebar/statusbar panel bg
	BgHover   lipgloss.Color // hover/key badge bg
	Fg        lipgloss.Color // primary foreground
	Selection    lipgloss.Color // selected item background
	SelectionDim lipgloss.Color // dimmer selection for summary lines
	Blue         lipgloss.Color // primary accent
	Green     lipgloss.Color // active, success
	Amber     lipgloss.Color // waiting, warning
	Red       lipgloss.Color // danger, kill
	Gray      lipgloss.Color // secondary text
	Purple    lipgloss.Color // model, secondary accent
	Cyan      lipgloss.Color // values, highlights
	Border    lipgloss.Color // borders, separators
	Dim       lipgloss.Color // faint elements
	DimText   lipgloss.Color // closed session text
}

// Themes is the registry of all built-in palettes.
var Themes = map[string]Palette{
	"tokyo-night": {
		Name:      "Tokyo Night",
		Bg:        lipgloss.Color("#16161e"),
		BgPanel:   lipgloss.Color("#1a1a2e"),
		BgHover:   lipgloss.Color("#1e2240"),
		Fg:        lipgloss.Color("#c0caf5"),
		Selection:    lipgloss.Color("#2a3a5e"),
		SelectionDim: lipgloss.Color("#243350"),
		Blue:         lipgloss.Color("#7aa2f7"),
		Green:        lipgloss.Color("#9ece6a"),
		Amber:        lipgloss.Color("#e0af68"),
		Red:          lipgloss.Color("#f7768e"),
		Gray:         lipgloss.Color("#565f89"),
		Purple:       lipgloss.Color("#bb9af7"),
		Cyan:         lipgloss.Color("#7dcfff"),
		Border:       lipgloss.Color("#333333"),
		Dim:          lipgloss.Color("#444444"),
		DimText:      lipgloss.Color("#787c99"),
	},
	"catppuccin-mocha": {
		Name:      "Catppuccin Mocha",
		Bg:        lipgloss.Color("#1e1e2e"),
		BgPanel:   lipgloss.Color("#181825"),
		BgHover:   lipgloss.Color("#313244"),
		Fg:        lipgloss.Color("#cdd6f4"),
		Selection:    lipgloss.Color("#313244"),
		SelectionDim: lipgloss.Color("#2a2b3c"),
		Blue:         lipgloss.Color("#89b4fa"),
		Green:        lipgloss.Color("#a6e3a1"),
		Amber:        lipgloss.Color("#fab387"),
		Red:          lipgloss.Color("#f38ba8"),
		Gray:         lipgloss.Color("#6c7086"),
		Purple:       lipgloss.Color("#cba6f7"),
		Cyan:         lipgloss.Color("#89dceb"),
		Border:       lipgloss.Color("#45475a"),
		Dim:          lipgloss.Color("#45475a"),
		DimText:      lipgloss.Color("#9399b2"),
	},
	"catppuccin-latte": {
		Name:      "Catppuccin Latte",
		Bg:        lipgloss.Color("#eff1f5"),
		BgPanel:   lipgloss.Color("#e6e9ef"),
		BgHover:   lipgloss.Color("#dce0e8"),
		Fg:        lipgloss.Color("#4c4f69"),
		Selection:    lipgloss.Color("#dce0e8"),
		SelectionDim: lipgloss.Color("#e4e7ee"),
		Blue:         lipgloss.Color("#1e66f5"),
		Green:        lipgloss.Color("#40a02b"),
		Amber:        lipgloss.Color("#df8e1d"),
		Red:          lipgloss.Color("#d20f39"),
		Gray:         lipgloss.Color("#8c8fa1"),
		Purple:       lipgloss.Color("#8839ef"),
		Cyan:         lipgloss.Color("#04a5e5"),
		Border:       lipgloss.Color("#ccd0da"),
		Dim:          lipgloss.Color("#ccd0da"),
		DimText:      lipgloss.Color("#7c7f93"),
	},
	"dracula": {
		Name:      "Dracula",
		Bg:        lipgloss.Color("#282a36"),
		BgPanel:   lipgloss.Color("#21222c"),
		BgHover:   lipgloss.Color("#343746"),
		Fg:        lipgloss.Color("#f8f8f2"),
		Selection:    lipgloss.Color("#44475a"),
		SelectionDim: lipgloss.Color("#3b3e50"),
		Blue:         lipgloss.Color("#6272a4"),
		Green:        lipgloss.Color("#50fa7b"),
		Amber:        lipgloss.Color("#ffb86c"),
		Red:          lipgloss.Color("#ff5555"),
		Gray:         lipgloss.Color("#6272a4"),
		Purple:       lipgloss.Color("#bd93f9"),
		Cyan:         lipgloss.Color("#8be9fd"),
		Border:       lipgloss.Color("#44475a"),
		Dim:          lipgloss.Color("#44475a"),
		DimText:      lipgloss.Color("#6272a4"),
	},
	"nord": {
		Name:      "Nord",
		Bg:        lipgloss.Color("#2e3440"),
		BgPanel:   lipgloss.Color("#272c36"),
		BgHover:   lipgloss.Color("#3b4252"),
		Fg:        lipgloss.Color("#eceff4"),
		Selection:    lipgloss.Color("#3b4252"),
		SelectionDim: lipgloss.Color("#333a49"),
		Blue:         lipgloss.Color("#81a1c1"),
		Green:        lipgloss.Color("#a3be8c"),
		Amber:        lipgloss.Color("#ebcb8b"),
		Red:          lipgloss.Color("#bf616a"),
		Gray:         lipgloss.Color("#616e88"),
		Purple:       lipgloss.Color("#b48ead"),
		Cyan:         lipgloss.Color("#88c0d0"),
		Border:       lipgloss.Color("#3b4252"),
		Dim:          lipgloss.Color("#3b4252"),
		DimText:      lipgloss.Color("#616e88"),
	},
	"one-dark": {
		Name:      "One Dark",
		Bg:        lipgloss.Color("#282c34"),
		BgPanel:   lipgloss.Color("#21252b"),
		BgHover:   lipgloss.Color("#2c313a"),
		Fg:        lipgloss.Color("#abb2bf"),
		Selection:    lipgloss.Color("#3e4451"),
		SelectionDim: lipgloss.Color("#363b47"),
		Blue:         lipgloss.Color("#61afef"),
		Green:        lipgloss.Color("#98c379"),
		Amber:        lipgloss.Color("#e5c07b"),
		Red:          lipgloss.Color("#e06c75"),
		Gray:         lipgloss.Color("#5c6370"),
		Purple:       lipgloss.Color("#c678dd"),
		Cyan:         lipgloss.Color("#56b6c2"),
		Border:       lipgloss.Color("#3e4451"),
		Dim:          lipgloss.Color("#3e4451"),
		DimText:      lipgloss.Color("#636d83"),
	},
	"gruvbox": {
		Name:      "Gruvbox Dark",
		Bg:        lipgloss.Color("#282828"),
		BgPanel:   lipgloss.Color("#1d2021"),
		BgHover:   lipgloss.Color("#3c3836"),
		Fg:        lipgloss.Color("#ebdbb2"),
		Selection:    lipgloss.Color("#504945"),
		SelectionDim: lipgloss.Color("#45403c"),
		Blue:         lipgloss.Color("#83a598"),
		Green:        lipgloss.Color("#b8bb26"),
		Amber:        lipgloss.Color("#fabd2f"),
		Red:          lipgloss.Color("#fb4934"),
		Gray:         lipgloss.Color("#928374"),
		Purple:       lipgloss.Color("#d3869b"),
		Cyan:         lipgloss.Color("#8ec07c"),
		Border:       lipgloss.Color("#504945"),
		Dim:          lipgloss.Color("#504945"),
		DimText:      lipgloss.Color("#a89984"),
	},
	"solarized-dark": {
		Name:      "Solarized Dark",
		Bg:        lipgloss.Color("#002b36"),
		BgPanel:   lipgloss.Color("#073642"),
		BgHover:   lipgloss.Color("#073642"),
		Fg:        lipgloss.Color("#839496"),
		Selection:    lipgloss.Color("#073642"),
		SelectionDim: lipgloss.Color("#062e38"),
		Blue:         lipgloss.Color("#268bd2"),
		Green:        lipgloss.Color("#859900"),
		Amber:        lipgloss.Color("#b58900"),
		Red:          lipgloss.Color("#dc322f"),
		Gray:         lipgloss.Color("#657b83"),
		Purple:       lipgloss.Color("#6c71c4"),
		Cyan:         lipgloss.Color("#2aa198"),
		Border:       lipgloss.Color("#073642"),
		Dim:          lipgloss.Color("#073642"),
		DimText:      lipgloss.Color("#586e75"),
	},
	"rose-pine": {
		Name:      "Rose Pine",
		Bg:        lipgloss.Color("#191724"),
		BgPanel:   lipgloss.Color("#1f1d2e"),
		BgHover:   lipgloss.Color("#26233a"),
		Fg:        lipgloss.Color("#e0def4"),
		Selection:    lipgloss.Color("#26233a"),
		SelectionDim: lipgloss.Color("#201e32"),
		Blue:         lipgloss.Color("#9ccfd8"),
		Green:        lipgloss.Color("#31748f"),
		Amber:        lipgloss.Color("#f6c177"),
		Red:          lipgloss.Color("#eb6f92"),
		Gray:         lipgloss.Color("#6e6a86"),
		Purple:       lipgloss.Color("#c4a7e7"),
		Cyan:         lipgloss.Color("#9ccfd8"),
		Border:       lipgloss.Color("#403d52"),
		Dim:          lipgloss.Color("#403d52"),
		DimText:      lipgloss.Color("#908caa"),
	},
	"kanagawa": {
		Name:      "Kanagawa",
		Bg:        lipgloss.Color("#1f1f28"),
		BgPanel:   lipgloss.Color("#16161d"),
		BgHover:   lipgloss.Color("#2a2a37"),
		Fg:        lipgloss.Color("#dcd7ba"),
		Selection:    lipgloss.Color("#2d4f67"),
		SelectionDim: lipgloss.Color("#26445b"),
		Blue:         lipgloss.Color("#7e9cd8"),
		Green:        lipgloss.Color("#76946a"),
		Amber:        lipgloss.Color("#dca561"),
		Red:          lipgloss.Color("#c34043"),
		Gray:         lipgloss.Color("#727169"),
		Purple:       lipgloss.Color("#957fb8"),
		Cyan:         lipgloss.Color("#6a9589"),
		Border:       lipgloss.Color("#363646"),
		Dim:          lipgloss.Color("#363646"),
		DimText:      lipgloss.Color("#54546d"),
	},
}

// Named returns a palette by name, falling back to tokyo-night if not found.
func Named(name string) Palette {
	if p, ok := Themes[name]; ok {
		return p
	}
	return Themes["tokyo-night"]
}

// ThemeNames returns the list of available theme names, sorted.
func ThemeNames() []string {
	names := make([]string, 0, len(Themes))
	for k := range Themes {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
