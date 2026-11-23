package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/attention"
)

func TestHooksStatusExitCodes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	exe := filepath.Join(dir, "naviclaude")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	devnull, _ := os.Open(os.DevNull)
	stdout := os.Stdout
	os.Stdout = devnull
	t.Cleanup(func() { os.Stdout = stdout; devnull.Close() })

	if rc := hooksStatus(settings); rc != 0 {
		t.Errorf("not installed: rc=%d, want 0", rc)
	}
	if err := attention.InstallHooks(settings, exe); err != nil {
		t.Fatal(err)
	}
	if rc := hooksStatus(settings); rc != 0 {
		t.Errorf("current: rc=%d, want 0", rc)
	}
	data, _ := os.ReadFile(settings)
	stamp := "# naviclaude-hook v" + strconv.Itoa(attention.HookVersion)
	if err := os.WriteFile(settings, []byte(strings.ReplaceAll(string(data), stamp, "# naviclaude-hook")), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := hooksStatus(settings); rc != 1 {
		t.Errorf("outdated: rc=%d, want 1", rc)
	}
	if err := attention.InstallHooks(settings, exe); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if rc := hooksStatus(settings); rc != 1 {
		t.Errorf("broken: rc=%d, want 1", rc)
	}
}
