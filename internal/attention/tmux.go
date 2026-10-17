package attention

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// tmux user options naviClaude keeps on panes, windows and the server.
const (
	optAttn      = "@naviclaude_attn"      // pane: "waiting" | "done" | ""
	optAttnSU    = "@naviclaude_attn_su"   // pane: native statusUpdatedAt the attention was raised for
	optToken     = "@naviclaude_token"     // pane: id of the watcher that owns the attention
	optBusy      = "@naviclaude_busy"      // pane: id of the watcher tracking a running turn
	optMarker    = "@naviclaude_marker"    // window: marker currently appended to the name
	optOrigName  = "@naviclaude_orig_name" // window: name before marking
	optOrigAuto  = "@naviclaude_orig_auto" // window: local automatic-rename before marking ("" = inherited)
	optLastRing  = "@naviclaude_last_ring" // server: unix ms of the last sound, for the cooldown
	fieldSep     = "\x1f"
	paneStateFmt = "#{window_id}" + fieldSep + "#{" + optAttn + "}" + fieldSep + "#{" + optAttnSU + "}" + fieldSep +
		"#{" + optToken + "}" + fieldSep + "#{pane_active}" + fieldSep + "#{window_active}" + fieldSep +
		"#{session_attached}" + fieldSep + "#{session_name}" + fieldSep + "#{" + optBusy + "}"
	windowStateFmt = "#{window_name}" + fieldSep + "#{" + optMarker + "}" + fieldSep + "#{" + optOrigName + "}" + fieldSep +
		"#{" + optOrigAuto + "}"
)

// paneState is one pane's attention options plus what tmux knows about whether
// it is on screen.
type paneState struct {
	Window   string
	Attn     string
	AttnSU   int64
	Token    string
	OnScreen bool // active pane of the active window of an attached session
	Session  string
	Busy     string
}

// paneMarks is what one pane contributes to its window's marker.
type paneMarks struct {
	Attn string
	Busy string
}

// windowState is a window's name and naviClaude's marker bookkeeping.
type windowState struct {
	Name     string
	Marker   string
	OrigName string
	OrigAuto string
}

// tmuxAPI is the tmux surface the attention logic needs. Option values of ""
// unset the option.
type tmuxAPI interface {
	Pane(pane string) (paneState, error)
	SetPaneOptions(pane string, kv map[string]string) error
	WindowPanes(window string) ([]paneMarks, error)
	Window(window string) (windowState, error)
	LocalAutoRename(window string) string
	RenameWindow(window, name string) error
	SetWindowOptions(window string, kv map[string]string) error
	SessionFocused(session string) bool
	LastRing() int64
	SetLastRing(ms int64)
}

// execTmux runs the real tmux binary against the server in $TMUX.
type execTmux struct{}

func (execTmux) run(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (t execTmux) Pane(pane string) (paneState, error) {
	out, err := t.run("display-message", "-p", "-t", pane, paneStateFmt)
	if err != nil {
		return paneState{}, err
	}
	f := strings.Split(out, fieldSep)
	if len(f) != 9 || f[0] == "" {
		return paneState{}, fmt.Errorf("tmux: pane %s not found", pane)
	}
	su, _ := strconv.ParseInt(f[2], 10, 64)
	attached, _ := strconv.Atoi(f[6])
	return paneState{
		Window:   f[0],
		Attn:     f[1],
		AttnSU:   su,
		Token:    f[3],
		OnScreen: f[4] == "1" && f[5] == "1" && attached > 0,
		Session:  f[7],
		Busy:     f[8],
	}, nil
}

func (t execTmux) setOptions(scope, target string, kv map[string]string) error {
	for k, v := range kv {
		var err error
		if v == "" {
			_, err = t.run("set-option", "-q", scope+"u", "-t", target, k)
		} else {
			_, err = t.run("set-option", "-q", scope, "-t", target, k, v)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (t execTmux) SetPaneOptions(pane string, kv map[string]string) error {
	return t.setOptions("-p", pane, kv)
}

func (t execTmux) SetWindowOptions(window string, kv map[string]string) error {
	return t.setOptions("-w", window, kv)
}

func (t execTmux) WindowPanes(window string) ([]paneMarks, error) {
	out, err := t.run("list-panes", "-t", window, "-F", "#{"+optAttn+"}"+fieldSep+"#{"+optBusy+"}")
	if err != nil {
		return nil, err
	}
	var marks []paneMarks
	for _, line := range strings.Split(out, "\n") {
		attn, busy, _ := strings.Cut(line, fieldSep)
		marks = append(marks, paneMarks{Attn: attn, Busy: busy})
	}
	return marks, nil
}

func (t execTmux) Window(window string) (windowState, error) {
	out, err := t.run("display-message", "-p", "-t", window, windowStateFmt)
	if err != nil {
		return windowState{}, err
	}
	f := strings.Split(out, fieldSep)
	if len(f) != 4 {
		return windowState{}, fmt.Errorf("tmux: window %s not found", window)
	}
	return windowState{Name: f[0], Marker: f[1], OrigName: f[2], OrigAuto: f[3]}, nil
}

// LocalAutoRename returns the window's own automatic-rename value, or "" when
// it inherits the global one.
func (t execTmux) LocalAutoRename(window string) string {
	out, _ := t.run("show-options", "-wqv", "-t", window, "automatic-rename")
	return strings.TrimSpace(out)
}

func (t execTmux) RenameWindow(window, name string) error {
	_, err := t.run("rename-window", "-t", window, name)
	return err
}

// SessionFocused reports whether a client showing session has terminal focus.
// Without focus-events tmux cannot know, so any attached client counts.
func (t execTmux) SessionFocused(session string) bool {
	out, err := t.run("list-clients", "-F", "#{client_session}"+fieldSep+"#{client_flags}")
	if err != nil {
		return false
	}
	focusEvents, _ := t.run("show-options", "-gqv", "focus-events")
	for _, line := range strings.Split(out, "\n") {
		name, flags, ok := strings.Cut(line, fieldSep)
		if !ok || name != session {
			continue
		}
		if strings.TrimSpace(focusEvents) != "on" || strings.Contains(","+flags+",", ",focused,") {
			return true
		}
	}
	return false
}

func (t execTmux) LastRing() int64 {
	out, _ := t.run("show-options", "-sqv", optLastRing)
	ms, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	return ms
}

func (t execTmux) SetLastRing(ms int64) {
	_, _ = t.run("set-option", "-sq", optLastRing, strconv.FormatInt(ms, 10))
}
