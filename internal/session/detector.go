package session

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tomhalo/naviclaude/internal/tmux"
)

// DefaultProcessNames is the default list of process names that indicate a
// Claude session is running in a tmux pane.
var DefaultProcessNames = []string{"claude"}

// Detector discovers active Claude sessions from tmux panes by inspecting the
// process tree of each pane.
type Detector struct {
	tmuxClient   *tmux.Client
	processNames []string
}

// NewDetector creates a Detector that uses the given tmux client and matches
// pane processes against processNames. If processNames is nil or empty, the
// DefaultProcessNames list is used.
func NewDetector(client *tmux.Client, processNames []string) *Detector {
	if len(processNames) == 0 {
		processNames = DefaultProcessNames
	}
	return &Detector{
		tmuxClient:   client,
		processNames: processNames,
	}
}

// Detect returns all active sessions found by walking the process tree of
// every tmux pane.
func (d *Detector) Detect() ([]*Session, error) {
	panes, err := d.tmuxClient.ListPanes()
	if err != nil {
		return nil, fmt.Errorf("detector: list panes: %w", err)
	}

	var sessions []*Session
	for _, pane := range panes {
		s := d.sessionFromPane(pane)
		if s == nil {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// sessionFromPane checks whether a pane is running a Claude process (directly
// or as a descendant), and if so returns a Session for it.
func (d *Detector) sessionFromPane(pane tmux.PaneInfo) *Session {
	// First check the pane's direct current command.
	if d.matchesProcessName(pane.CurrentCommand) {
		return d.buildSession(pane, pane.PID)
	}

	// Walk the process tree from the pane's PID to find a matching descendant.
	claudePID := d.findClaudePID(pane.PID)
	if claudePID == 0 {
		return nil
	}
	return d.buildSession(pane, claudePID)
}

// buildSession constructs a Session from PaneInfo. The claudePID is the PID
// of the actual Claude process (which may be a grandchild of the pane's shell).
func (d *Detector) buildSession(pane tmux.PaneInfo, claudePID int) *Session {
	s := &Session{
		TmuxSession:  pane.SessionName,
		TmuxTarget:   pane.Target,
		CWD:          pane.CurrentPath,
		Status:       StatusActive,
		LastActivity: time.Now(),
		ProjectName:  filepath.Base(pane.CurrentPath),
		PID:          claudePID,
	}

	// Populate CPU and memory from ps.
	cpu, mem := queryProcessStats(claudePID)
	s.CPU = cpu
	s.Memory = mem

	return s
}

// matchesProcessName reports whether name (typically from pane_current_command)
// matches any entry in the detector's processNames list. The comparison is
// case-insensitive and also checks the basename to handle absolute paths.
func (d *Detector) matchesProcessName(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	for _, want := range d.processNames {
		if base == strings.ToLower(want) {
			return true
		}
	}
	return false
}

// findClaudePID performs a depth-first walk of the process tree rooted at
// rootPID using `pgrep -P` and returns the PID of the first descendant whose
// process name matches. Returns 0 if no match is found.
func (d *Detector) findClaudePID(rootPID int) int {
	return d.walkTree(rootPID, 6) // max depth to avoid runaway recursion
}

func (d *Detector) walkTree(pid int, depth int) int {
	if depth == 0 {
		return 0
	}
	children := childPIDs(pid)
	for _, child := range children {
		name := processName(child)
		if d.matchesProcessName(name) {
			return child
		}
		if found := d.walkTree(child, depth-1); found != 0 {
			return found
		}
	}
	return 0
}

// childPIDs returns the direct child PIDs of the given parent using pgrep -P.
func childPIDs(parentPID int) []int {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(parentPID)).Output()
	if err != nil {
		// pgrep exits 1 when no children are found; not a real error.
		return nil
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p, err := strconv.Atoi(line)
		if err == nil {
			pids = append(pids, p)
		}
	}
	return pids
}

// processName returns the executable name of the given PID using ps.
// Returns an empty string on any error.
func processName(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(filepath.Base(string(out)))
}

// queryProcessStats returns the CPU % and resident memory in MB for a process.
// On any error, both values are zero.
func queryProcessStats(pid int) (cpu float64, memMB float64) {
	// ps -p <pid> -o %cpu,rss  (rss is in KB on macOS/Linux)
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,rss=").Output()
	if err != nil {
		return 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) < 2 {
		return 0, 0
	}
	cpu, _ = strconv.ParseFloat(parts[0], 64)
	rssKB, _ := strconv.ParseFloat(parts[1], 64)
	memMB = rssKB / 1024.0
	return cpu, memMB
}
