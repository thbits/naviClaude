package preview

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tomhalo/naviclaude/internal/tmux"
)

// Passthrough forwards Bubble Tea key events to a tmux pane via send-keys.
// The intercepted keys Tab, Ctrl+], and Ctrl+f are NOT forwarded; callers are
// responsible for handling those before calling SendKey.
type Passthrough struct {
	tmuxClient *tmux.Client
}

// NewPassthrough creates a Passthrough backed by the given tmux client.
func NewPassthrough(client *tmux.Client) *Passthrough {
	return &Passthrough{tmuxClient: client}
}

// SendKey translates a Bubble Tea KeyMsg and forwards it to the target pane.
// Returns an error if the underlying tmux send-keys command fails.
func (p *Passthrough) SendKey(target string, msg tea.KeyMsg) error {
	keys := translateKey(msg)
	if keys == "" {
		return nil
	}
	return p.tmuxClient.SendKeys(target, keys)
}

// translateKey converts a Bubble Tea KeyMsg into a tmux send-keys argument.
// Printable rune sequences are sent as literal strings. Special keys are
// translated to tmux named key strings.
func translateKey(msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyRunes:
		return string(msg.Runes)

	case tea.KeySpace:
		return " "

	case tea.KeyEnter:
		return "Enter"

	case tea.KeyBackspace:
		return "BSpace"

	case tea.KeyEsc:
		return "Escape"

	case tea.KeyUp:
		return "Up"

	case tea.KeyDown:
		return "Down"

	case tea.KeyLeft:
		return "Left"

	case tea.KeyRight:
		return "Right"

	case tea.KeyHome:
		return "Home"

	case tea.KeyEnd:
		return "End"

	case tea.KeyPgUp:
		return "PPage"

	case tea.KeyPgDown:
		return "NPage"

	case tea.KeyDelete:
		return "DC"

	case tea.KeyInsert:
		return "IC"

	case tea.KeyShiftTab:
		return "BTab"

	case tea.KeyF1:
		return "F1"

	case tea.KeyF2:
		return "F2"

	case tea.KeyF3:
		return "F3"

	case tea.KeyF4:
		return "F4"

	case tea.KeyF5:
		return "F5"

	case tea.KeyF6:
		return "F6"

	case tea.KeyF7:
		return "F7"

	case tea.KeyF8:
		return "F8"

	case tea.KeyF9:
		return "F9"

	case tea.KeyF10:
		return "F10"

	case tea.KeyF11:
		return "F11"

	case tea.KeyF12:
		return "F12"

	// Control characters
	case tea.KeyCtrlA:
		return "C-a"

	case tea.KeyCtrlB:
		return "C-b"

	case tea.KeyCtrlC:
		return "C-c"

	case tea.KeyCtrlD:
		return "C-d"

	case tea.KeyCtrlE:
		return "C-e"

	case tea.KeyCtrlF:
		return "C-f"

	case tea.KeyCtrlG:
		return "C-g"

	case tea.KeyCtrlH:
		return "C-h"

	case tea.KeyCtrlJ:
		return "C-j"

	case tea.KeyCtrlK:
		return "C-k"

	case tea.KeyCtrlL:
		return "C-l"

	case tea.KeyCtrlN:
		return "C-n"

	case tea.KeyCtrlO:
		return "C-o"

	case tea.KeyCtrlP:
		return "C-p"

	case tea.KeyCtrlQ:
		return "C-q"

	case tea.KeyCtrlR:
		return "C-r"

	case tea.KeyCtrlS:
		return "C-s"

	case tea.KeyCtrlT:
		return "C-t"

	case tea.KeyCtrlU:
		return "C-u"

	case tea.KeyCtrlV:
		return "C-v"

	case tea.KeyCtrlW:
		return "C-w"

	case tea.KeyCtrlX:
		return "C-x"

	case tea.KeyCtrlY:
		return "C-y"

	case tea.KeyCtrlZ:
		return "C-z"

	default:
		return ""
	}
}
