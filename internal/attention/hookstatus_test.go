package attention

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckHooksStates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	exe := fakeExe(t, dir, "it's here/naviclaude")

	if r, err := CheckHooks(settings); err != nil || r.State != HooksNotInstalled {
		t.Fatalf("missing file: %+v %v", r, err)
	}

	if err := InstallHooks(settings, exe); err != nil {
		t.Fatal(err)
	}
	r, err := CheckHooks(settings)
	if err != nil || r.State != HooksCurrent || r.Exe != exe || r.Version != HookVersion || len(r.Events) != len(hookEvents) {
		t.Fatalf("fresh install: %+v %v", r, err)
	}

	// An install from before the stamp existed.
	data, _ := os.ReadFile(settings)
	old := strings.ReplaceAll(string(data), hookMarker+" v"+strconv.Itoa(HookVersion), hookMarker)
	if err := os.WriteFile(settings, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := CheckHooks(settings); r.State != HooksOutdated || r.Version != 1 {
		t.Errorf("unstamped: %+v", r)
	}
	// Reinstalling replaces the old entries instead of adding beside them.
	if err := InstallHooks(settings, exe); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(settings)
	if n := strings.Count(string(data), hookMarker); n != len(hookEvents) {
		t.Errorf("reinstall left %d entries, want %d", n, len(hookEvents))
	}
	if r, _ := CheckHooks(settings); r.State != HooksCurrent {
		t.Errorf("after reinstall: %+v", r)
	}

	// A changed matcher means the hook set differs from this version's.
	data, _ = os.ReadFile(settings)
	changed := strings.Replace(string(data), `"AskUserQuestion|ExitPlanMode"`, `"AskUserQuestion"`, 1)
	if err := os.WriteFile(settings, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := CheckHooks(settings); r.State != HooksOutdated {
		t.Errorf("changed matcher: %+v", r)
	}

	if err := InstallHooks(settings, exe); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exe, 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := CheckHooks(settings); r.State != HooksBroken {
		t.Errorf("non-executable binary: %+v", r)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if r, _ := CheckHooks(settings); r.State != HooksBroken || !strings.Contains(strings.Join(r.Problems, ";"), "not found") {
		t.Errorf("missing binary: %+v", r)
	}
}

func TestCheckHooksFlagsCellarPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	exe := fakeExe(t, dir, "Cellar/naviclaude/1.0/bin/naviclaude")
	if err := InstallHooks(settings, exe); err != nil {
		t.Fatal(err)
	}
	// With no stable symlink a reinstall could not do better, so it is not flagged.
	if r, _ := CheckHooks(settings); r.State != HooksCurrent {
		t.Errorf("Cellar path without symlink: %+v", r)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(dir, "bin", "naviclaude")); err != nil {
		t.Fatal(err)
	}
	if r, _ := CheckHooks(settings); r.State != HooksOutdated {
		t.Errorf("Cellar path with symlink: %+v", r)
	}
}

func TestCommandExeRoundTrip(t *testing.T) {
	for _, exe := range []string{"/usr/local/bin/naviclaude", "/opt/navi claude/naviclaude", "/a/it's/b'"} {
		if got := commandExe(HookCommand(exe)); got != exe {
			t.Errorf("commandExe(HookCommand(%q)) = %q", exe, got)
		}
	}
	if got := commandExe("/bin/naviclaude hook # naviclaude-hook"); got != "/bin/naviclaude" {
		t.Errorf("unquoted = %q", got)
	}
	if v := commandVersion("'x' hook # naviclaude-hook"); v != 1 {
		t.Errorf("bare marker version = %d", v)
	}
	for _, c := range []string{"'x' hook # naviclaude-hook v", "'x' hook # naviclaude-hook vx", "'x' hook # naviclaude-hook v0"} {
		if v := commandVersion(c); v != 1 {
			t.Errorf("commandVersion(%q) = %d, want 1", c, v)
		}
	}
	if v := commandVersion(HookCommand("x")); v != HookVersion {
		t.Errorf("stamped version = %d", v)
	}
}

func TestStableExecutablePrefersBrewSymlink(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	cellar := fakeExe(t, dir, "Cellar/naviclaude/1.2.3/bin/naviclaude")
	bin := filepath.Join(dir, "bin", "naviclaude")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Cellar/naviclaude/1.2.3/bin/naviclaude", bin); err != nil {
		t.Fatal(err)
	}
	onPath := func(string) (string, error) { return bin, nil }
	notFound := func(string) (string, error) { return "", exec.ErrNotFound }
	other := func(string) (string, error) { return fakeExe(t, dir, "elsewhere/naviclaude"), nil }

	if got := StableExecutable(cellar, onPath); got != bin {
		t.Errorf("with symlink on PATH = %q, want %q", got, bin)
	}
	// Off PATH, the prefix's bin symlink still wins over the Cellar path.
	if got := StableExecutable(cellar, notFound); got != bin {
		t.Errorf("symlink off PATH = %q, want %q", got, bin)
	}
	// A different naviclaude on PATH is not ours to store.
	if got := StableExecutable(cellar, other); got != bin {
		t.Errorf("other on PATH = %q, want prefix symlink %q", got, bin)
	}
	plain := fakeExe(t, dir, "go/bin/naviclaude")
	if got := StableExecutable(plain, other); got != plain {
		t.Errorf("unrelated PATH entry = %q, want %q", got, plain)
	}
	if got := StableExecutable(bin, onPath); got != bin {
		t.Errorf("already stable = %q", got)
	}
}
