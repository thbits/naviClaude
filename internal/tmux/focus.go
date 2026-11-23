package tmux

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const (
	// optInstance marks the pane a running dashboard lives in; its value is that process's pid.
	optInstance = "@naviclaude"
	// optSelect is a request, written by `naviclaude focus`, for a running dashboard to select a session.
	optSelect = "@naviclaude_select"
)

// ClientInfo is one attached tmux client and what it is looking at.
type ClientInfo struct {
	Name     string
	Activity int64 // unix time of the last input
	Session  string
	Pane     string // the pane the client's window has active
}

// Instance is a pane a running dashboard marked as its own.
type Instance struct {
	Pane    string
	Session string
	PID     int
	Marked  bool // false for a dashboard found only by its command, e.g. an older build
}

// MarkInstance records pid as the dashboard running in pane.
func (c *Client) MarkInstance(pane string, pid int) error {
	if err := exec.Command("tmux", "set-option", "-pq", "-t", pane, optInstance, strconv.Itoa(pid)).Run(); err != nil {
		return fmt.Errorf("tmux set-option -t %s: %w", pane, err)
	}
	return nil
}

// UnmarkInstance clears pane's dashboard mark when it still names pid, so a
// later dashboard in the same pane keeps its own.
func (c *Client) UnmarkInstance(pane string, pid int) {
	out, err := exec.Command("tmux", "show-options", "-pqv", "-t", pane, optInstance).Output()
	if err != nil || strings.TrimSpace(string(out)) != strconv.Itoa(pid) {
		return
	}
	_ = exec.Command("tmux", "set-option", "-pqu", "-t", pane, optInstance).Run()
}

// TakeSelect returns and clears the session `naviclaude focus` asked pane's dashboard to select.
func (c *Client) TakeSelect(pane string) string {
	// One invocation reads and clears, so a request is never seen twice.
	out, err := exec.Command("tmux", "show-options", "-pqv", "-t", pane, optSelect,
		";", "set-option", "-pqu", "-t", pane, optSelect).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RequestSelect asks the dashboard in pane to select target.
func (c *Client) RequestSelect(pane, target string) error {
	if out, err := exec.Command("tmux", "set-option", "-pq", "-t", pane, optSelect, target).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux set-option -t %s: %s", pane, strings.TrimSpace(string(out)))
	}
	return nil
}

// ListClients returns the attached clients.
func (c *Client) ListClients() ([]ClientInfo, error) {
	format := strings.Join([]string{"#{client_activity}", "#{client_name}", "#{client_session}", "#{pane_id}"}, FieldSep)
	out, err := exec.Command("tmux", "list-clients", "-F", format).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-clients: %w", err)
	}
	return parseClients(string(out)), nil
}

func parseClients(out string) []ClientInfo {
	var clients []ClientInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, FieldSep)
		if len(f) != 4 || f[1] == "" {
			continue
		}
		act, _ := strconv.ParseInt(f[0], 10, 64)
		clients = append(clients, ClientInfo{Activity: act, Name: f[1], Session: f[2], Pane: f[3]})
	}
	return clients
}

// ListInstances returns the panes running a dashboard, marked or not, ordered by pane id.
func (c *Client) ListInstances() ([]Instance, error) {
	format := strings.Join([]string{"#{pane_id}", "#{session_name}", "#{" + optInstance + "}", "#{pane_current_command}"}, FieldSep)
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", format).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}
	return parseInstances(string(out)), nil
}

func parseInstances(out string) []Instance {
	var found []Instance
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, FieldSep)
		if len(f) < 3 {
			continue
		}
		if f[2] != "" {
			pid, _ := strconv.Atoi(f[2])
			found = append(found, Instance{Pane: f[0], Session: f[1], PID: pid, Marked: true})
		} else if len(f) > 3 && f[3] == "naviclaude" {
			found = append(found, Instance{Pane: f[0], Session: f[1]})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Pane < found[j].Pane })
	return found
}

// Reveal shows pane on client: its session, its window, then the pane itself.
// A modal floating pane is already its window's active pane, so selecting it just confirms that.
func (c *Client) Reveal(client, pane string) error {
	args := []string{"switch-client"}
	if client != "" {
		args = append(args, "-c", client)
	}
	args = append(args, "-t", pane, ";", "select-window", "-t", pane, ";", "select-pane", "-t", pane)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux switch-client -t %s: %s", pane, strings.TrimSpace(string(out)))
	}
	return nil
}
