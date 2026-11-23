package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
)

// loadKeys loads a config file holding yaml, as New does.
func loadKeys(t *testing.T, yaml string) config.KeyBindings {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Keys
}

// Saved configs spell out every key, so an old file still says detail: "d"
// although d is the done filter now.
func TestOldConfigDetailKeyMovesToShiftD(t *testing.T) {
	kb := loadKeys(t, "keys:\n  detail: \"d\"\n  stats: \"s\"\n")
	if kb.Detail != "d" || kb.FilterDone != "d" {
		t.Fatalf("precondition: loaded detail = %q, filter_done = %q; want both d", kb.Detail, kb.FilterDone)
	}
	km := KeyMapFromConfig(kb)
	if km.Detail != "D" || km.FilterDone != "d" {
		t.Errorf("detail = %q, filter_done = %q; want D and d", km.Detail, km.FilterDone)
	}
	if conflicts := ValidateKeys(kb); len(conflicts) != 0 {
		t.Errorf("the migrated keys are reported as a conflict: %v", conflicts)
	}
	if got := MigrateKeys(kb).Detail; got != "D" {
		t.Errorf("MigrateKeys detail = %q, want D so the next save writes it", got)
	}
}

func TestDetailKeyMigrationOnlyWhenDoneFilterOwnsD(t *testing.T) {
	// filter_done left unset means its default, d.
	if km := KeyMapFromConfig(config.KeyBindings{Detail: "d"}); km.Detail != "D" {
		t.Errorf("unset filter_done: detail = %q, want D", km.Detail)
	}
	// Moving the filter off d is how to keep detail there.
	kb := loadKeys(t, "keys:\n  detail: \"d\"\n  filter_done: \"x\"\n")
	if km := KeyMapFromConfig(kb); km.Detail != "d" || km.FilterDone != "x" {
		t.Errorf("detail = %q, filter_done = %q; want d and x kept", km.Detail, km.FilterDone)
	}
	if conflicts := ValidateKeys(kb); len(conflicts) != 0 {
		t.Errorf("unexpected conflicts: %v", conflicts)
	}
	// Any other detail key is the user's choice.
	if km := KeyMapFromConfig(config.KeyBindings{Detail: "o"}); km.Detail != "o" {
		t.Errorf("detail = %q, want o", km.Detail)
	}
}

func TestSpaceKeyNameMatchesTheKeyPress(t *testing.T) {
	want := tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}.String()
	if km := KeyMapFromConfig(config.DefaultConfig().Keys); km.NextAttention != want {
		t.Errorf("next_attention = %q, want %q (what a space press reports)", km.NextAttention, want)
	}
	if km := DefaultKeyMap(); km.NextAttention != want {
		t.Errorf("default next_attention = %q, want %q", km.NextAttention, want)
	}
	if km := KeyMapFromConfig(config.KeyBindings{NextAttention: "tab"}); km.NextAttention != "tab" {
		t.Errorf("next_attention = %q, want other key names untouched", km.NextAttention)
	}

	// "space" and " " are one key to the conflict check, which names it readably.
	conflicts := ValidateKeys(config.KeyBindings{NextAttention: "space", Stats: " "})
	if len(conflicts) != 1 || !strings.Contains(conflicts[0].Message, `"Space"`) {
		t.Errorf("conflicts = %v, want one naming Space", conflicts)
	}
}

// Space is labeled "Space"; a hint carrying that label must replay the key the
// list listens for.
func TestSpaceLabelReplaysTheKey(t *testing.T) {
	km := DefaultKeyMap()
	label := keyLabel(km.NextAttention)
	if label != "Space" {
		t.Fatalf("next-attention label = %q, want Space", label)
	}
	msg, ok := hintKeyMsg(label)
	if !ok || msg.String() != km.NextAttention {
		t.Errorf("clicking the hint sends %q (ok=%v), want %q", msg.String(), ok, km.NextAttention)
	}
}

// The new defaults must not shadow each other, the fixed list keys, or the
// keys a kept preview search takes over.
func TestNewDefaultKeysAreFree(t *testing.T) {
	km := DefaultKeyMap()
	fixed := map[string]string{"j": "down", "k": "up", "g": "first row", "G": "last row", "{": "previous group", "}": "next group",
		"ctrl+u": "scroll up", "ctrl+d": "scroll down", "up": "up", "down": "down", KeyThemePicker: "theme picker"}
	for name, key := range map[string]string{
		"next_attention": km.NextAttention, "filter_waiting": km.FilterWaiting, "filter_done": km.FilterDone,
		"filter_working": km.FilterWorking, "filter_idle": km.FilterIdle, "preview_search": km.PreviewSearch, "detail": km.Detail,
	} {
		if what, taken := fixed[key]; taken {
			t.Errorf("%s default %q is the fixed %s key", name, key, what)
		}
	}
	if conflicts := ValidateKeys(config.DefaultConfig().Keys); len(conflicts) != 0 {
		t.Errorf("default keys conflict: %v", conflicts)
	}
}

func TestHelpListsTheNewKeys(t *testing.T) {
	km := DefaultKeyMap()
	var all []string
	for _, b := range km.HelpBindings() {
		all = append(all, b.Key+" "+b.Desc)
	}
	help := strings.Join(all, "\n")
	for _, want := range []string{"Space Next session", "w d a i Only waiting", "S Search the preview", "D Detail"} {
		if !strings.Contains(help, want) {
			t.Errorf("help is missing %q:\n%s", want, help)
		}
	}
}
