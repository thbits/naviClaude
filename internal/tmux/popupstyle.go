package tmux

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// PopupStyle draws a popup and its border in the terminal's own colours. "default"
// can't: tmux resolves it to popup-style, which tmux 3.8 sets to a themed grey.
const PopupStyle = "fg=terminal,bg=terminal"

// runFunc runs one tmux command and returns its stdout.
type runFunc func(args ...string) (string, error)

// popupEnv is what restyleOwnPopup reads about its own process.
type popupEnv struct {
	tmux     string // $TMUX: socket path, server pid, session id
	tmuxPane string // $TMUX_PANE, set in every pane (floating ones too) but not in popups
	pid      int
	parentOf func(pid int) (int, bool)
}

// RestyleOwnPopup redraws the display-popup naviClaude runs in with the terminal's
// colours on tmux 3.8+. It does nothing unless every check says this is our popup.
func (c *Client) RestyleOwnPopup(parentOf func(pid int) (int, bool)) {
	env := popupEnv{tmux: os.Getenv("TMUX"), tmuxPane: os.Getenv("TMUX_PANE"), pid: os.Getpid(), parentOf: parentOf}
	restyleOwnPopup(env, func(args ...string) (string, error) {
		out, err := exec.Command("tmux", args...).Output()
		return string(out), err
	})
}

// restyleOwnPopup returns the display-popup argv it ran, or nil when it ran none.
func restyleOwnPopup(env popupEnv, run runFunc) []string {
	if env.tmuxPane != "" {
		return nil
	}
	serverPID, sessionID, ok := parseTMUXEnv(env.tmux)
	if !ok {
		return nil
	}
	session := "$" + strconv.Itoa(sessionID)
	version, err := run("display-message", "-p", "-t", session, "#{version}")
	if err != nil || !versionAtLeast(version, 3, 8) {
		return nil
	}
	out, err := run("list-panes", "-a", "-F", "#{pane_pid}")
	if err != nil || !isPopupJob(env.pid, serverPID, parsePIDs(out), env.parentOf) {
		return nil
	}
	// A popup job's session is the only link to its client, so it must be unambiguous.
	out, err = run("list-clients", "-t", session, "-F", "#{client_name}")
	clients := strings.Fields(out)
	if err != nil || len(clients) != 1 {
		return nil
	}
	args := restylePopupArgs(clients[0])
	if _, err := run(args...); err != nil {
		return nil
	}
	return args
}

// restylePopupArgs modifies the client's open popup. Without -E/-k/-N/-b/-B tmux keeps
// its close and border settings; the oversized -h fails instead of opening a new popup.
func restylePopupArgs(client string) []string {
	return []string{"display-popup", "-c", client, "-w", "99999", "-h", "99999",
		"-s", PopupStyle, "-S", PopupStyle, "true"}
}

// parseTMUXEnv splits $TMUX ("socket,pid,session"); the socket path may hold commas.
func parseTMUXEnv(v string) (serverPID, sessionID int, ok bool) {
	f := strings.Split(v, ",")
	if len(f) < 3 {
		return 0, 0, false
	}
	pid, errP := strconv.Atoi(f[len(f)-2])
	sid, errS := strconv.Atoi(f[len(f)-1])
	if errP != nil || errS != nil || pid <= 1 || sid < 0 {
		return 0, 0, false
	}
	return pid, sid, true
}

func parsePIDs(out string) map[int]bool {
	pids := make(map[int]bool)
	for _, f := range strings.Fields(out) {
		if pid, err := strconv.Atoi(f); err == nil {
			pids[pid] = true
		}
	}
	return pids
}

// isPopupJob reports whether pid descends from the tmux server without passing
// through a pane process, which only a popup job's process tree does.
func isPopupJob(pid, serverPID int, panePIDs map[int]bool, parentOf func(int) (int, bool)) bool {
	for i := 0; i < 32; i++ {
		if panePIDs[pid] {
			return false
		}
		ppid, ok := parentOf(pid)
		if !ok || ppid <= 1 {
			return false
		}
		if ppid == serverPID {
			return true
		}
		pid = ppid
	}
	return false
}
