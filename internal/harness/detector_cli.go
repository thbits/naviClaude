package harness

// IncludeOwnPane stops Detect from skipping the pane naviClaude runs in, which
// for the CLI is the calling agent's own session.
func (d *Detector) IncludeOwnPane() {
	d.selfPID, d.parentPID = -1, -1
}
