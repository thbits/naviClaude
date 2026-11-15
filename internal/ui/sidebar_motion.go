package ui

// GoToRow moves the cursor to the 1-based row n, clamped to the list (vim's {count}G).
func (m *SidebarModel) GoToRow(n int) {
	m.cursor = n - 1
	m.clampCursor()
	m.updateTracked()
}

// JumpGroup moves to the |delta|-th group header after (delta > 0) or before the
// cursor, stopping at the list's last or first row when the headers run out.
func (m *SidebarModel) JumpGroup(delta int) {
	if len(m.flatItems) == 0 || delta == 0 {
		return
	}
	step, left := 1, delta
	if delta < 0 {
		step, left = -1, -delta
	}
	target := m.cursor
	for i := m.cursor + step; i >= 0 && i < len(m.flatItems) && left > 0; i += step {
		target = i
		if m.flatItems[i].isGroup {
			left--
		}
	}
	m.cursor = target
	m.updateTracked()
	m.syncViewport()
}
