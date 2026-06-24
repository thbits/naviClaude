package session

// SubtreeCPU returns the summed CPU% of pid and all of its descendants in the
// process tree. A Claude session that is running a tool (build, test, fetch)
// often shows ~0% CPU on the claude PID itself while a child process burns CPU,
// so the subtree total is the reliable "is it working" signal.
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
