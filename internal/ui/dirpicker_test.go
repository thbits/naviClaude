package ui

import (
	"os"
	"path/filepath"
	"testing"
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
		)
		want := []string{"/proj", "/proj/a", "/proj/b", "/z/one", "/sess/x"}
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
		)
		want := []string{"/proj", "/proj/a", "/z/one", "/sess/x"}
		if g := paths(got); !equalStrings(g, want) {
			t.Errorf("paths = %v, want %v", g, want)
		}
	})

	t.Run("empty base is skipped", func(t *testing.T) {
		got := buildCandidates("", nil, []string{"/z/one"}, nil)
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

	m.Show("/proj", []string{"/sess/one"})

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
		m.Descend() // into /proj/sub1
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
		m.Parent() // back to /proj
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
