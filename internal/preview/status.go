package preview

import (
	"github.com/thbits/naviClaude/internal/session"
)

// StatusDetector detects the status of a Claude Code session by analyzing
// the captured pane content. It only performs prompt detection -- it does NOT
// try to detect "active" vs "idle" via content comparison, because spinners,
// status bars, timestamps, and cursor blink cause constant false-positive
// content changes that result in status flickering.
//
// Active status comes from the session Detector (process is running).
// This detector only transitions to Waiting when a prompt is visible.
type StatusDetector struct{}

// NewStatusDetector creates a StatusDetector.
func NewStatusDetector() *StatusDetector {
	return &StatusDetector{}
}

// DetectFromContent inspects captured pane content and returns Waiting if a
// known prompt pattern is detected, or StatusActive otherwise. The caller
// (capturePreviewCmd) only applies Waiting status updates to avoid overriding
// the detector's authoritative Active status, so the StatusActive branch is
// effectively a "not waiting" sentinel the caller ignores. The return type and
// the Active return are kept as-is rather than collapsed to a bool: the value
// flows into previewCaptureMsg.status alongside the native-status path (which
// can yield Active/Idle), and the handler keys solely off StatusWaiting.
// Changing this return type would force a wider, non-behavior-identical rework
// of that message contract, so it is left intact (see findings note).
func (d *StatusDetector) DetectFromContent(target, content string) session.SessionStatus {
	if session.ClassifyPaneContent(content) == session.SignalWaiting {
		return session.StatusWaiting
	}
	return session.StatusActive
}

// Reset is a no-op (kept for interface compatibility).
func (d *StatusDetector) Reset(target string) {}
