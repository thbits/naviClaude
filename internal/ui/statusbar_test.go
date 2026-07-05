package ui

import (
	"strings"
	"testing"
)

func TestStatusBar_UpdateLabel(t *testing.T) {
	sb := NewStatusBar(120, "1.2.3")

	if strings.Contains(sb.View(), "update available") {
		t.Fatal("label should be absent before SetUpdateAvailable(true)")
	}

	sb.SetUpdateAvailable(true)
	if !strings.Contains(sb.View(), "update available") {
		t.Error("label should be present after SetUpdateAvailable(true)")
	}
	if !strings.Contains(sb.View(), "1.2.3") {
		t.Error("version should still be present alongside the label")
	}
}
