package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSaveKeepsCommentsOrderAndUnknownKeys(t *testing.T) {
	p := writeTemp(t, `# my config
theme: "nord" # favourite
my_custom_key: hello
keys:
  # quit binding
  quit: "x"
  future_key: y
sidebar_width: 40
process_names: ["claude", "cc"]
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SidebarWidth = 55
	cfg.Keys.Detail = "I"
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := readFile(t, p)
	for _, want := range []string{"# my config", `theme: "nord" # favourite`, "my_custom_key: hello",
		"# quit binding", `quit: "x"`, "future_key: y", "sidebar_width: 55", "detail: I", `["claude", "cc"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("saved file lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "theme") > strings.Index(got, "my_custom_key") ||
		strings.Index(got, "my_custom_key") > strings.Index(got, "sidebar_width") {
		t.Errorf("key order changed:\n%s", got)
	}
	// Unchanged defaults are not spelled out.
	if strings.Contains(got, "popup_width") {
		t.Errorf("default popup_width written:\n%s", got)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, cfg) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", loaded, cfg)
	}
	if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 kept", info.Mode().Perm())
	}
}

func TestSaveNewFileWritesOnlyChanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Theme = "nord"
	cfg.Notifications.Enabled = false
	if err := Save(cfg, p); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	want := "theme: nord\nnotifications:\n  enabled: false\n"
	if got != want {
		t.Errorf("saved:\n%s\nwant:\n%s", got, want)
	}
	if w := UnknownKeys(p); len(w) != 0 {
		t.Errorf("unknown keys after Save: %v", w)
	}
}

func TestSaveRefusesUnparsableFile(t *testing.T) {
	for name, content := range map[string]string{
		"syntax": "theme: [unclosed\n",
		"type":   "sidebar_width: wide\ntheme: nord\n",
		"list":   "- a\n- b\n",
	} {
		t.Run(name, func(t *testing.T) {
			p := writeTemp(t, content)
			if err := Save(DefaultConfig(), p); err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Fatalf("Save err = %v, want refusal", err)
			}
			if got := readFile(t, p); got != content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

func TestSaveEmptyAndCommentOnlyFiles(t *testing.T) {
	for _, content := range []string{"", "\n  \n", "# nothing yet\n"} {
		p := writeTemp(t, content)
		cfg := DefaultConfig()
		cfg.SidebarWidth = 42
		if err := Save(cfg, p); err != nil {
			t.Fatalf("Save(%q): %v", content, err)
		}
		got := readFile(t, p)
		if !strings.Contains(got, "sidebar_width: 42") {
			t.Errorf("Save(%q) wrote %q", content, got)
		}
		if strings.HasPrefix(content, "#") && !strings.Contains(got, "# nothing yet") {
			t.Errorf("comment lost: %q", got)
		}
	}
}

func TestSaveFillsNullSection(t *testing.T) {
	p := writeTemp(t, "keys:\ntheme: nord\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Keys.Quit = "Q"
	if err := Save(cfg, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Keys.Quit != "Q" || loaded.Theme != "nord" {
		t.Errorf("loaded %+v", loaded)
	}
}

func TestSaveFollowsSymlinkAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles.yaml")
	if err := os.WriteFile(real, []byte("theme: nord\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Theme = "dracula"
	if err := Save(cfg, link); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced: %v %v", fi, err)
	}
	if got := readFile(t, real); got != "theme: dracula\n" {
		t.Errorf("target = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestUnknownKeys(t *testing.T) {
	p := writeTemp(t, `theme: nord
them: nord
keys:
  detial: d
  quit: q
notifications:
  sond: x
  enabled: true
process_names: [claude]
`)
	cfg, warns, err := LoadWithWarnings(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "nord" || cfg.Keys.Quit != "q" {
		t.Errorf("values not loaded: %+v", cfg)
	}
	want := []string{"unknown key them", "unknown key keys.detial", "unknown key notifications.sond"}
	if !reflect.DeepEqual(warns, want) {
		t.Errorf("warnings = %v, want %v", warns, want)
	}
}

// Every field of a saved default config must be recognised, so new fields
// are known automatically.
func TestUnknownKeysKnowsEveryField(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	data, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if w := UnknownKeys(p); len(w) != 0 {
		t.Errorf("unknown keys in a full default config: %v", w)
	}
	if w := UnknownKeys(filepath.Join(t.TempDir(), "missing.yaml")); w != nil {
		t.Errorf("missing file warnings = %v", w)
	}
}

func TestSaveSinceKeepsHandEditsMadeAfterLoad(t *testing.T) {
	p := writeTemp(t, "theme: nord\nsidebar_width: 40\n")
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	// The user edits the file while the app runs, then the app saves another field.
	if err := os.WriteFile(p, []byte("theme: dracula\nsidebar_width: 40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loaded
	cfg.SidebarWidth = 55
	if err := SaveSince(loaded, cfg, p); err != nil {
		t.Fatalf("SaveSince: %v", err)
	}
	got := readFile(t, p)
	if !strings.Contains(got, "theme: dracula") || !strings.Contains(got, "sidebar_width: 55") {
		t.Errorf("want the hand edit kept and the app's change written:\n%s", got)
	}
}
