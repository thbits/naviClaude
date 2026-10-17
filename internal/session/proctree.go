package session

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessTree holds the entire process tree from a single ps call, including
// CPU and RSS stats to avoid per-session ps calls.
type ProcessTree struct {
	children map[int][]int   // parent -> child PIDs
	names    map[int]string  // PID -> process name (basename)
	ppid     map[int]int     // PID -> parent PID
	cpu      map[int]float64 // PID -> CPU %
	rss      map[int]float64 // PID -> RSS in KB
}

// Proc is one row of the process table.
type Proc struct {
	PID, PPID int
	Name      string  // executable basename
	CPU       float64 // %CPU
	RSSKB     float64 // resident set size in KB
}

// NewProcessTree builds a tree from an explicit process list.
func NewProcessTree(procs []Proc) *ProcessTree {
	tree := &ProcessTree{
		children: make(map[int][]int),
		names:    make(map[int]string),
		ppid:     make(map[int]int),
		cpu:      make(map[int]float64),
		rss:      make(map[int]float64),
	}
	for _, p := range procs {
		tree.names[p.PID] = p.Name
		tree.ppid[p.PID] = p.PPID
		tree.children[p.PPID] = append(tree.children[p.PPID], p.PID)
		tree.cpu[p.PID] = p.CPU
		tree.rss[p.PID] = p.RSSKB
	}
	return tree
}

// BuildProcessTree runs a single `ps -eo pid=,ppid=,%cpu=,rss=,comm=` command
// and builds an in-memory process tree with stats, avoiding per-PID ps calls.
func BuildProcessTree() *ProcessTree {
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,%cpu=,rss=,comm=").Output()
	if err != nil {
		return NewProcessTree(nil)
	}
	var procs []Proc
	for _, line := range strings.Split(string(out), "\n") {
		// Fields: PID PPID %CPU RSS COMM (comm may contain spaces for full path)
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		cpuVal, _ := strconv.ParseFloat(fields[2], 64)
		rssVal, _ := strconv.ParseFloat(fields[3], 64)
		comm := strings.Join(fields[4:], " ")
		procs = append(procs, Proc{PID: pid, PPID: ppid, Name: filepath.Base(comm), CPU: cpuVal, RSSKB: rssVal})
	}
	return NewProcessTree(procs)
}

// Name returns the executable basename of pid, or "" when unknown.
func (t *ProcessTree) Name(pid int) string { return t.names[pid] }

// Parent returns pid's parent PID and whether pid was in the snapshot.
func (t *ProcessTree) Parent(pid int) (int, bool) {
	ppid, ok := t.ppid[pid]
	return ppid, ok
}

// Stats returns CPU % and memory MB for a PID from the pre-built tree.
func (t *ProcessTree) Stats(pid int) (cpu float64, memMB float64) {
	return t.cpu[pid], t.rss[pid] / 1024.0
}

// ParentAppName returns pid's parent program (e.g. "nvim") when the agent runs as
// its subprocess, or "" for a shell or tmux parent.
func (t *ProcessTree) ParentAppName(pid int) string {
	ppid, ok := t.ppid[pid]
	if !ok || ppid <= 1 {
		return ""
	}
	name := strings.TrimPrefix(strings.ToLower(t.names[ppid]), "-") // -zsh → zsh
	if isShellOrTmux(name) {
		return ""
	}
	return t.names[ppid]
}

func isShellOrTmux(name string) bool {
	switch name {
	case "zsh", "bash", "sh", "fish", "dash", "ksh", "csh", "tcsh", "nu", "login":
		return true
	}
	return strings.HasPrefix(name, "tmux")
}

// IsAncestorOf reports whether ancestorPID is an ancestor of descendantPID
// using the pre-built process tree.
func (t *ProcessTree) IsAncestorOf(ancestorPID, descendantPID int) bool {
	pid := descendantPID
	for i := 0; i < 10; i++ {
		ppid, ok := t.ppid[pid]
		if !ok || ppid <= 1 {
			return false
		}
		if ppid == ancestorPID {
			return true
		}
		pid = ppid
	}
	return false
}

// FindMatchingDescendant returns the first descendant of rootPID, depth-first,
// whose name satisfies matchFunc, or 0.
func (t *ProcessTree) FindMatchingDescendant(rootPID int, matchFunc func(string) bool, maxDepth int) int {
	if maxDepth == 0 {
		return 0
	}
	for _, child := range t.children[rootPID] {
		name := t.names[child]
		if matchFunc(name) {
			return child
		}
		if found := t.FindMatchingDescendant(child, matchFunc, maxDepth-1); found != 0 {
			return found
		}
	}
	return 0
}
