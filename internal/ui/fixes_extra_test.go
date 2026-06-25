package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/session"
)

// TestTruncateDisplay_RuneSafeNoPanic complements TestTruncateDisplay and
// TestTruncateDisplayNoPanicSmallWidths. It targets fix (4): truncation must be
// rune-safe and must never panic for any width <= 0 on multibyte input, including
// wide (CJK) graphemes and multi-byte emoji that a naive byte slice would split.
func TestTruncateDisplay_RuneSafeNoPanic(t *testing.T) {
	inputs := []string{
		"",
		"абвгде",      // 2-byte Cyrillic runes
		"日本語テキスト",     // wide (2-cell) CJK
		"a日b本c",       // mixed ASCII + wide
		"🚀🔥✨🎉",        // multi-byte emoji
		"café résumé", // combining-ish accented Latin
		"plain ascii string here",
	}

	// fix (4) specifically: maxLen <= 0 must return "" and never panic, even for
	// multibyte input. Also sweep small positive widths where only the ellipsis or
	// nothing fits.
	for _, in := range inputs {
		for w := -5; w <= 8; w++ {
			got := truncateDisplay(in, w) // must not panic
			if w <= 0 {
				if got != "" {
					t.Errorf("truncateDisplay(%q, %d) = %q, want empty for non-positive width", in, w, got)
				}
				continue
			}
			// For positive widths the result must never exceed the requested
			// display width, and must be valid (no mid-rune split -> StringWidth
			// is well defined).
			if gw := ansi.StringWidth(got); gw > w {
				t.Errorf("truncateDisplay(%q, %d) width = %d, exceeds max %d", in, w, gw, w)
			}
		}
	}
}

// TestTruncateDisplay_WideRuneNotSplit targets fix (4): a wide grapheme that does
// not fit must be dropped whole (replaced by the ellipsis), never sliced into an
// invalid byte sequence.
func TestTruncateDisplay_WideRuneNotSplit(t *testing.T) {
	// "日本" is two 2-cell graphemes (total width 4). Truncating to width 3 cannot
	// fit the second wide rune alongside the ellipsis, so the result must be the
	// first wide rune plus the ellipsis (width 3), and must remain valid UTF-8.
	got := truncateDisplay("日本", 3)
	if w := ansi.StringWidth(got); w > 3 {
		t.Fatalf("truncateDisplay(\"日本\", 3) = %q width %d, exceeds 3", got, w)
	}
	if got == "" {
		t.Fatalf("truncateDisplay(\"日本\", 3) unexpectedly empty")
	}
	// Round-tripping through []rune proves no byte was severed mid-rune.
	for _, r := range got {
		if r == '�' {
			t.Fatalf("truncateDisplay produced a replacement char (mid-rune split): %q", got)
		}
	}
}

// TestRunSearch_NonEmptyQueryResultsNotAliased targets the second half of fix (5):
// Results() must hand out a slice the caller can mutate without corrupting the
// model's source session list -- on the SCORED (non-empty-query) path, not just
// the empty-query copy path that TestResultsEmptyQueryIsCopy already covers.
func TestRunSearch_NonEmptyQueryResultsNotAliased(t *testing.T) {
	sessions := []*session.Session{
		{ID: "aaa", ProjectName: "alpha"},
		{ID: "bbb", ProjectName: "alpha"},
		{ID: "ccc", ProjectName: "alpha"},
	}

	m := NewSearch()
	m.SetSessions(sessions)
	m.Activate()
	m.input.SetValue("alpha")
	m.runSearch()

	results := m.Results()
	if len(results) != len(sessions) {
		t.Fatalf("got %d results, want %d", len(results), len(sessions))
	}

	// Reverse the returned slice in place. The internal results slice is built
	// fresh from the scored list, so the source m.sessions must be untouched.
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	wantSource := []string{"aaa", "bbb", "ccc"}
	gotSource := resultIDs(sessions)
	if !equalStrings(gotSource, wantSource) {
		t.Errorf("m.sessions corrupted by reslicing Results(): %v, want %v", gotSource, wantSource)
	}
}

// TestRunSearch_StableTieOrderRepeated targets the first half of fix (5): tied
// scores must produce a byte-for-byte identical order on every run. This is a
// stronger repetition than TestRunSearchStableTieOrder and seeds the sessions in
// reverse-ID order to prove the tiebreaker (ID ascending) actually reorders them.
func TestRunSearch_StableTieOrderRepeated(t *testing.T) {
	sessions := []*session.Session{
		{ID: "zzz", ProjectName: "proj", DisplayName: "z"},
		{ID: "mmm", ProjectName: "proj", DisplayName: "m"},
		{ID: "aaa", ProjectName: "proj", DisplayName: "a"},
	}

	m := NewSearch()
	m.SetSessions(sessions)
	m.Activate()
	m.input.SetValue("proj")

	want := []string{"aaa", "mmm", "zzz"} // tiebreaker is ID ascending
	for run := 0; run < 100; run++ {
		m.runSearch()
		got := resultIDs(m.Results())
		if !equalStrings(got, want) {
			t.Fatalf("run %d: order = %v, want stable %v", run, got, want)
		}
	}
}
