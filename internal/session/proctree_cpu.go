package session

// SubtreeCPU sums %cpu over pid's subtree, since a running tool burns CPU in a
// child. ps reports a lifetime average, so this is only a coarse fallback signal.
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
