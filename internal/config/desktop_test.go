package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadNotificationsDesktop(t *testing.T) {
	def := "off"
	if runtime.GOOS == "darwin" {
		def = "system"
	}
	if got := DefaultConfig().Notifications.Desktop; got != def {
		t.Errorf("default desktop = %q, want %q", got, def)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	cases := map[string]string{
		"off": "off", `"off"`: "off", "false": "off", "no": "off",
		"system": "system", "true": "system", "Terminal": "terminal",
		"bogus": def, `""`: def,
	}
	for in, want := range cases {
		if err := os.WriteFile(path, []byte("notifications:\n  desktop: "+in+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("desktop: %s: %v", in, err)
		}
		if cfg.Notifications.Desktop != want {
			t.Errorf("desktop: %s loaded as %q, want %q", in, cfg.Notifications.Desktop, want)
		}
	}
}

func TestNotificationsClickDefaultsAndNormalizes(t *testing.T) {
	for in, want := range map[string]string{"": "naviclaude", "pane": "pane", " Pane ": "pane", "naviclaude": "naviclaude", "bogus": "naviclaude"} {
		p := filepath.Join(t.TempDir(), "c.yaml")
		if err := os.WriteFile(p, []byte("notifications:\n  click: \""+in+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Notifications.Click != want {
			t.Errorf("click %q -> %q, want %q", in, cfg.Notifications.Click, want)
		}
	}
}
