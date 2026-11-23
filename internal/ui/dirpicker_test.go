package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/junegunn/fzf/src/util"
)

func paths(cands []DirCandidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Path
	}
	return out
}

func TestBuildCandidates(t *testing.T) {
	t.Run("base first, then subdirs, zoxide, sessions; order preserved", func(t *testing.T) {
		got := buildCandidates(
			"/proj",
			[]string{"/proj/a", "/proj/b"},
			[]string{"/z/one"},
			[]string{"/sess/x"},
			[]string{"/proj/a", "/home/w"},
		)
		want := []string{"/proj", "/proj/a", "/proj/b", "/z/one", "/sess/x", "/home/w"}
		if g := paths(got); !equalStrings(g, want) {
			t.Errorf("paths = %v, want %v", g, want)
		}
		if got[0].Source != "base" {
			t.Errorf("first source = %q, want base", got[0].Source)
		}
		if got[3].Source != "zoxide" {
			t.Errorf("zoxide source = %q, want zoxide", got[3].Source)
		}
	})

	t.Run("dedups by absolute path across sources", func(t *testing.T) {
		got := buildCandidates(
			"/proj",
			[]string{"/proj/a"},
			[]string{"/proj/a", "/z/one"}, // /proj/a duplicates a subdir
			[]string{"/proj", "/sess/x"},  // /proj duplicates base
			nil,
		)
		want := []string{"/proj", "/proj/a", "/z/one", "/sess/x"}
		if g := paths(got); !equalStrings(g, want) {
			t.Errorf("paths = %v, want %v", g, want)
		}
	})

	t.Run("empty base is skipped", func(t *testing.T) {
		got := buildCandidates("", nil, []string{"/z/one"}, nil, nil)
		if g := paths(got); !equalStrings(g, []string{"/z/one"}) {
			t.Errorf("paths = %v, want [/z/one]", g)
		}
	})
}

func TestFilterCandidates(t *testing.T) {
	cands := []DirCandidate{
		{Path: "/proj", Source: "base"},
		{Path: "/proj/naviClaude", Source: "subdir"},
		{Path: "/z/opmed-charts", Source: "zoxide"},
	}
	filterCandidates := func(query string, cands []DirCandidate) []DirCandidate {
		return filterPaths(query, cands)
	}

	t.Run("empty query returns all unchanged", func(t *testing.T) {
		got := filterCandidates("  ", cands)
		if len(got) != len(cands) || got[0].Path != "/proj" {
			t.Errorf("got %v, want unchanged", paths(got))
		}
	})

	t.Run("fuzzy query surfaces the match", func(t *testing.T) {
		got := filterCandidates("navi", cands)
		if len(got) == 0 || got[0].Path != "/proj/naviClaude" {
			t.Errorf("got %v, want naviClaude first", paths(got))
		}
	})

	t.Run("non-matching query returns empty", func(t *testing.T) {
		if got := filterCandidates("zzzznothere", cands); len(got) != 0 {
			t.Errorf("got %v, want empty", paths(got))
		}
	})
}

func TestExpandTildeAndAbsClean(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := expandTilde("~"); got != home {
		t.Errorf("expandTilde(~) = %q, want %q", got, home)
	}
	if got := expandTilde("~/projects"); got != filepath.Join(home, "projects") {
		t.Errorf("expandTilde(~/projects) = %q, want %q", got, filepath.Join(home, "projects"))
	}
	if got := absClean("/already/abs"); got != "/already/abs" {
		t.Errorf("absClean(/already/abs) = %q, want /already/abs", got)
	}
	if got := absClean(""); got != "" {
		t.Errorf("absClean(\"\") = %q, want empty", got)
	}
}

func TestListSubdirs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"bbb", "aaa", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := listSubdirs(root)
	want := []string{filepath.Join(root, "aaa"), filepath.Join(root, "bbb")}
	if !equalStrings(got, want) {
		t.Errorf("listSubdirs = %v, want %v (sorted, no hidden, no files)", got, want)
	}
}

func TestDirPickerNavigation(t *testing.T) {
	m := NewDirPicker()
	// Inject deterministic sources (no real FS / zoxide).
	m.zoxideFn = func() []string { return []string{"/z/alpha"} }
	m.listDirsFn = func(base string) []string {
		return []string{filepath.Join(base, "sub1"), filepath.Join(base, "sub2")}
	}

	// runLoad executes the (now async) candidate-load command returned by the
	// navigation methods and applies its dirCandidatesMsg, simulating the app's
	// Update loop. The lookups are synchronous via the injected fns here.
	m.walkFn = func(string, int) []string { return nil }
	runLoad := func(cmd tea.Cmd) {
		if cmd == nil {
			t.Fatal("expected a candidate-load command, got nil")
		}
		drainCmd(&m, cmd)
	}

	runLoad(m.Show("/proj", []string{"/sess/one"}))

	t.Run("base preselected", func(t *testing.T) {
		if got := m.Selected(); got != "/proj" {
			t.Errorf("Selected() = %q, want /proj", got)
		}
	})

	t.Run("move down selects first subdir", func(t *testing.T) {
		m.MoveDown()
		if got := m.Selected(); got != "/proj/sub1" {
			t.Errorf("Selected() = %q, want /proj/sub1", got)
		}
	})

	t.Run("descend makes the subdir the new base", func(t *testing.T) {
		runLoad(m.Descend()) // into /proj/sub1
		if got := m.Selected(); got != "/proj/sub1" {
			t.Errorf("after descend Selected() = %q, want /proj/sub1 (new base)", got)
		}
		// Its subdirs come from the injected lister rooted at the new base.
		m.MoveDown()
		if got := m.Selected(); got != "/proj/sub1/sub1" {
			t.Errorf("Selected() = %q, want /proj/sub1/sub1", got)
		}
	})

	t.Run("parent ascends", func(t *testing.T) {
		runLoad(m.Parent()) // back to /proj
		if got := m.Selected(); got != "/proj" {
			t.Errorf("after parent Selected() = %q, want /proj", got)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A long candidate list must not grow the popup past the height it was given.
func TestDirPickerFitsHeight(t *testing.T) {
	m := NewDirPicker()
	m.zoxideFn = func() []string { return nil }
	m.listDirsFn = func(base string) []string {
		var dirs []string
		for i := 0; i < 40; i++ {
			dirs = append(dirs, filepath.Join(base, fmt.Sprintf("d%02d", i)))
		}
		return dirs
	}
	m.walkFn = func(string, int) []string { return nil }
	m.SetSize(120, 20)
	drainCmd(&m, m.Show("/proj", nil))
	if got := lipgloss.Height(m.View()); got > 20 {
		t.Errorf("popup height = %d, want <= 20", got)
	}
}

// drainCmd runs cmd (expanding batches) and feeds each resulting message back
// into the picker, simulating the app's Update loop.
func drainCmd(m *DirPickerModel, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drainCmd(m, c)
		}
		return
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		return
	}
	*m, _ = m.Update(msg)
}

// typeQuery feeds query into the picker's input as one keystroke burst.
func typeQuery(m *DirPickerModel, query string) {
	*m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
}

// Without zoxide (a fresh machine) nested directories must still be found.
func TestDirPickerFindsNestedDirsWithoutZoxide(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	deep := filepath.Join(base, "work", "repos", "naviClaude")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewDirPicker()
	m.zoxideFn = func() []string { return nil }
	drainCmd(&m, m.Show(base, nil))
	typeQuery(&m, "navi")

	if got := m.Selected(); got != deep {
		t.Errorf("Selected() = %q, want %q", got, deep)
	}
}

// filterPaths runs the picker's fzf filter over cands matched on their paths.
func filterPaths(query string, cands []DirCandidate) []DirCandidate {
	chars := make([]util.Chars, len(cands))
	for i, c := range cands {
		chars[i] = util.ToChars([]byte(c.Path))
	}
	return filterCandidates(newFzfMatcher(), parseFzfQuery(query), cands, chars)
}

func TestFilterCandidatesFzfSemantics(t *testing.T) {
	cands := []DirCandidate{
		{Path: "~/git/navigation-old/src", Source: "walk"},
		{Path: "~/personal/git/naviClaude/internal/ui", Source: "walk"},
		{Path: "~/personal/git/naviClaude", Source: "walk"},
		{Path: "~/git/opmed-charts", Source: "walk"},
		{Path: "~/git/opmed-tf", Source: "walk"},
	}
	cases := []struct {
		query string
		want  []string
	}{
		// fzf ranking: tighter match wins, ties go to the shorter path.
		{"naviclaude", []string{"~/personal/git/naviClaude", "~/personal/git/naviClaude/internal/ui"}},
		// Path tiebreak: a match starting in the basename beats a shorter path.
		{"navi", []string{"~/personal/git/naviClaude", "~/git/navigation-old/src", "~/personal/git/naviClaude/internal/ui"}},
		// Smart case: an uppercase letter makes the term case-sensitive.
		{"naviC", []string{"~/personal/git/naviClaude", "~/personal/git/naviClaude/internal/ui"}},
		{"NAVIC", nil},
		// Space-separated terms are ANDed, in any order.
		{"charts opmed", []string{"~/git/opmed-charts"}},
		{"'opmed !charts", []string{"~/git/opmed-tf"}},
		{"tf$", []string{"~/git/opmed-tf"}},
		{"^~/git/opmed", []string{"~/git/opmed-tf", "~/git/opmed-charts"}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			if got := paths(filterPaths(tc.query, cands)); !equalStrings(got, tc.want) {
				t.Errorf("filter(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestWalkDirs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{
		"a/a1/a2/a3",
		"z/z1",
		".hidden/inside",
		"node_modules/pkg",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "z"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(filepath.Join(locked, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	skip := func(_, name string) bool { return walkSkipNames[name] }
	rel := func(ps []string) []string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i], _ = filepath.Rel(root, p)
		}
		return out
	}

	t.Run("breadth-first, skips hidden, junk and unreadable, lists symlinks", func(t *testing.T) {
		got := rel(walkDirs([]walkRoot{{root, 10}}, 100, skip))
		want := []string{"a", "link", "locked", "z", "a/a1", "z/z1", "a/a1/a2", "a/a1/a2/a3"}
		if !equalStrings(got, want) {
			t.Errorf("walk = %v, want %v", got, want)
		}
	})

	t.Run("depth limit", func(t *testing.T) {
		got := rel(walkDirs([]walkRoot{{root, 2}}, 100, skip))
		want := []string{"a", "link", "locked", "z", "a/a1", "z/z1"}
		if !equalStrings(got, want) {
			t.Errorf("walk = %v, want %v", got, want)
		}
	})

	t.Run("cap keeps every shallow dir", func(t *testing.T) {
		got := rel(walkDirs([]walkRoot{{root, 10}}, 5, skip))
		want := []string{"a", "link", "locked", "z", "a/a1"}
		if !equalStrings(got, want) {
			t.Errorf("walk = %v, want %v", got, want)
		}
	})

	t.Run("second root adds dirs outside the first", func(t *testing.T) {
		other := t.TempDir()
		if err := os.MkdirAll(filepath.Join(other, "elsewhere"), 0o755); err != nil {
			t.Fatal(err)
		}
		got := walkDirs([]walkRoot{{filepath.Join(root, "z"), 5}, {other, 2}}, 100, skip)
		want := []string{filepath.Join(root, "z", "z1"), filepath.Join(other, "elsewhere")}
		if !equalStrings(got, want) {
			t.Errorf("walk = %v, want %v", got, want)
		}
	})
}

// Base must stay selectable before the slow walk lands, and the walk must not
// move a cursor the user already placed.
func TestDirPickerTwoStageLoad(t *testing.T) {
	m := NewDirPicker()
	m.zoxideFn = func() []string { return nil }
	m.listDirsFn = func(base string) []string { return []string{filepath.Join(base, "sub")} }
	m.walkFn = func(root string, _ int) []string {
		if root != "/proj" {
			return nil
		}
		return []string{filepath.Join(root, "sub", "deep")}
	}

	cmd := m.Show("/proj", nil)
	if got := m.Selected(); got != "/proj" {
		t.Fatalf("before any load Selected() = %q, want /proj", got)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Show cmd = %T, want tea.BatchMsg", cmd())
	}
	var msgs []tea.Msg
	var collect func(tea.Cmd)
	collect = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, cc := range b {
				collect(cc)
			}
			return
		}
		msgs = append(msgs, msg)
	}
	for _, c := range batch {
		collect(c)
	}
	var quick, walk, home tea.Msg
	for _, msg := range msgs {
		switch msg.(type) {
		case dirCandidatesMsg:
			quick = msg
		case dirWalkMsg:
			walk = msg
		case dirHomeWalkMsg:
			home = msg
		}
	}
	m, _ = m.Update(quick)
	m.MoveDown()
	m, _ = m.Update(walk)
	if got := m.Selected(); got != "/proj/sub" {
		t.Errorf("after walk Selected() = %q, want /proj/sub", got)
	}

	// Mid-query, a late walk result puts the cursor on the new best match.
	typeQuery(&m, "dee")
	m.SetCursor(len(m.filtered) - 1)
	m, _ = m.Update(home)
	if got := m.Selected(); got != "/proj/sub/deep" {
		t.Errorf("after home walk Selected() = %q, want /proj/sub/deep", got)
	}

	// Navigating keeps the $HOME walk and only re-walks the new base.
	homeCount := len(m.homeDirs)
	cmd = m.Descend()
	if homeCount == 0 || len(m.homeDirs) != homeCount {
		t.Errorf("home dirs = %d after descend, want %d (non-zero)", len(m.homeDirs), homeCount)
	}
	for _, c := range cmd().(tea.BatchMsg) {
		if _, ok := c().(dirHomeWalkMsg); ok {
			t.Error("descend re-walked $HOME")
		}
	}
}
