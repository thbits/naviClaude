package app

import (
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/config"
)

func TestValidateKeysNoConflicts(t *testing.T) {
	// The shipped defaults must not conflict with each other or reserved keys.
	def := config.DefaultConfig()
	if conflicts := ValidateKeys(def.Keys); len(conflicts) != 0 {
		t.Fatalf("expected no conflicts for default keys, got %v", conflicts)
	}

	// Empty bindings mean "use defaults" and should never be flagged.
	if conflicts := ValidateKeys(config.KeyBindings{}); len(conflicts) != 0 {
		t.Fatalf("expected no conflicts for empty bindings, got %v", conflicts)
	}
}

func TestValidateKeysDuplicateBinding(t *testing.T) {
	kb := config.DefaultConfig().Keys
	// Bind two distinct actions to the same key.
	kb.Detail = "x"
	kb.Stats = "x"

	conflicts := ValidateKeys(kb)
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly 1 conflict, got %d: %v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.Key != "x" {
		t.Errorf("conflict key = %q, want %q", c.Key, "x")
	}
	if !strings.Contains(c.Message, "detail") || !strings.Contains(c.Message, "stats") {
		t.Errorf("conflict message %q should mention both actions", c.Message)
	}
}

func TestValidateKeysReservedCollision(t *testing.T) {
	kb := config.DefaultConfig().Keys
	// Shadow the theme-picker reserved key.
	kb.Detail = KeyThemePicker

	conflicts := ValidateKeys(kb)
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly 1 conflict, got %d: %v", len(conflicts), conflicts)
	}
	if conflicts[0].Key != KeyThemePicker {
		t.Errorf("conflict key = %q, want %q", conflicts[0].Key, KeyThemePicker)
	}
	if !strings.Contains(conflicts[0].Message, "reserved") {
		t.Errorf("conflict message %q should mention it is reserved", conflicts[0].Message)
	}
}

func TestValidateKeysReservedTabAndEsc(t *testing.T) {
	// tab and esc are hardcoded; binding an action to either must be flagged.
	for _, key := range []string{KeyTab, KeyMenuCancel, KeyExitPassthrough2} {
		kb := config.KeyBindings{Jump: key}
		conflicts := ValidateKeys(kb)
		if len(conflicts) != 1 {
			t.Errorf("key %q: expected 1 conflict, got %d: %v", key, len(conflicts), conflicts)
			continue
		}
		if conflicts[0].Key != key {
			t.Errorf("key %q: conflict key = %q", key, conflicts[0].Key)
		}
	}
}

func TestKeyMapFromConfigTableDrive(t *testing.T) {
	// Verify the table-driven build still applies overrides and keeps defaults.
	kb := config.KeyBindings{
		Quit:   "Q",
		Detail: "D",
		// everything else empty -> default
	}
	km := KeyMapFromConfig(kb)
	if km.Quit != "Q" {
		t.Errorf("Quit = %q, want %q", km.Quit, "Q")
	}
	if km.Detail != "D" {
		t.Errorf("Detail = %q, want %q", km.Detail, "D")
	}
	def := DefaultKeyMap()
	if km.Search != def.Search {
		t.Errorf("Search = %q, want default %q", km.Search, def.Search)
	}
	if km.NewTmuxSession != def.NewTmuxSession {
		t.Errorf("NewTmuxSession = %q, want default %q", km.NewTmuxSession, def.NewTmuxSession)
	}
}
