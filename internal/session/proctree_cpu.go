package session

// SubtreeCPU returns the summed CPU% of pid and all of its descendants in the
// process tree. A Claude session that is running a tool (build, test, fetch)
// often shows ~0% CPU on the claude PID itself while a child process burns CPU,
// so the subtree total is the reliable "is it working" signal.
//
// LIMITATION: the per-PID %cpu values come from `ps`, which reports a LIFETIME
// AVERAGE (total CPU time over the whole process lifetime divided by elapsed
// time), not instantaneous usage. A long-lived process that was busy early and
// is now idle can still report a non-trivial %cpu, and a process that just
// started a burst of work can report a low %cpu. This signal is therefore a
// coarse fallback only -- it is intentionally NOT redesigned because Claude's
// own native status (see resolveStatus) is now the primary, authoritative
// "is it working" source, and this path is reached only for older Claude
// versions that do not write a native status.
func (t *ProcessTree) SubtreeCPU(pid int) float64 {
	if _, ok := t.cpu[pid]; !ok {
		if _, hasChildren := t.children[pid]; !hasChildren {
			return 0
		}
	}
	visited := make(map[int]bool)
	return t.subtreeCPU(pid, visited)
}

func (t *ProcessTree) subtreeCPU(pid int, visited map[int]bool) float64 {
	if visited[pid] {
		return 0
	}
	visited[pid] = true
	total := t.cpu[pid]
	for _, child := range t.children[pid] {
		total += t.subtreeCPU(child, visited)
	}
	return total
}
