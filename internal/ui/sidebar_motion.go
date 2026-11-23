package ui

// GoToRow moves the cursor to the 1-based row n, clamped to the list.
func (m *SidebarModel) GoToRow(n int) {
	m.cursor = n - 1
	m.clampCursor()
	m.updateTracked()
}

// GoToSession moves the cursor to the 1-based session n, skipping group headers
// and clamping to the last session (vim's {count}G).
func (m *SidebarModel) GoToSession(n int) {
	target := -1
	for i, it := range m.flatItems {
		if it.isGroup {
			continue
		}
		target = i
		if n--; n <= 0 {
			break
		}
	}
	if target < 0 {
		return
	}
	m.cursor = target
	m.updateTracked()
	m.syncViewport()
}

// MoveSessions moves the cursor to the |delta|-th session below (delta > 0) or
// above it, skipping group headers and stopping at the last session that way.
func (m *SidebarModel) MoveSessions(delta int) {
	if delta == 0 {
		return
	}
	step, left := 1, delta
	if delta < 0 {
		step, left = -1, -delta
	}
	target := m.cursor
	for i := m.cursor + step; i >= 0 && i < len(m.flatItems) && left > 0; i += step {
		if !m.flatItems[i].isGroup {
			target = i
			left--
		}
	}
	if target == m.cursor {
		return
	}
	m.cursor = target
	m.updateTracked()
	m.syncViewport()
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
