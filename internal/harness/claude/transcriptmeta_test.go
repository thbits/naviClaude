package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// lastMessageTime and lastAITitle wrap a full scan for lastactivity_test.go and aititle_test.go.
func lastMessageTime(path string) time.Time {
	m, err := scanTranscriptMeta(path, nil)
	if err != nil {
		return time.Time{}
	}
	return m.lastTS
}

func lastAITitle(path string) string {
	m, err := scanTranscriptMeta(path, nil)
	if err != nil {
		return ""
	}
	return m.aiTitle
}

func appendFile(t testing.TB, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func mustScan(t testing.TB, path string, prev *transcriptMeta) *transcriptMeta {
	t.Helper()
	m, err := scanTranscriptMeta(path, prev)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertSameMeta(t *testing.T, label string, got, want *transcriptMeta) {
	t.Helper()
	if !got.lastTS.Equal(want.lastTS) || got.aiTitle != want.aiTitle || got.offset != want.offset {
		t.Errorf("%s: got {ts %v, title %q, off %d}, want {ts %v, title %q, off %d}",
			label, got.lastTS, got.aiTitle, got.offset, want.lastTS, want.aiTitle, want.offset)
	}
}

var metaFixture = strings.Join([]string{
	`{"type":"user","timestamp":"2026-06-25T08:00:00.000Z"}`,
	`{"type":"ai-title","aiTitle":"First guess"}`,
	`{"type":"file-history-snapshot"}`,
	`{"type":"assistant","timestamp":"2026-06-25T10:00:00.000Z"}`,
	`not json`,
	``,
	`{"type":"user","timestamp":"2026-06-25T09:00:00.000Z"}`,
	`{"type":"ai-title","aiTitle":""}`,
	`{"type":"ai-title","aiTitle":"Final title"}`,
	`{"type":"custom-title"}`,
}, "\n") + "\n"

func TestTranscriptMetaIncrementalEqualsFull(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "full.jsonl")
	if err := os.WriteFile(full, []byte(metaFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	want := mustScan(t, full, nil)
	if want.aiTitle != "Final title" || want.offset != int64(len(metaFixture)) {
		t.Fatalf("full scan = %+v", want)
	}

	// Every byte split, including mid-record ones, must converge on the full scan.
	for i := 0; i <= len(metaFixture); i++ {
		path := filepath.Join(dir, fmt.Sprintf("split%d.jsonl", i))
		if err := os.WriteFile(path, []byte(metaFixture[:i]), 0o644); err != nil {
			t.Fatal(err)
		}
		prev := mustScan(t, path, nil)
		appendFile(t, path, metaFixture[i:])
		assertSameMeta(t, fmt.Sprintf("split at %d", i), mustScan(t, path, prev), want)
	}
}

func TestTranscriptMetaPartialLineCompleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	first := `{"type":"ai-title","aiTitle":"Old"}` + "\n"
	rec := `{"type":"ai-title","aiTitle":"New","timestamp":"2026-06-25T11:00:00Z"}` + "\n"
	half := len(rec) / 2
	if err := os.WriteFile(path, []byte(first+rec[:half]), 0o644); err != nil {
		t.Fatal(err)
	}

	m := mustScan(t, path, nil)
	if m.aiTitle != "Old" || m.offset != int64(len(first)) {
		t.Fatalf("half-written: title %q offset %d, want %q at %d", m.aiTitle, m.offset, "Old", len(first))
	}

	appendFile(t, path, rec[half:])
	m = mustScan(t, path, m)
	want, _ := time.Parse(time.RFC3339, "2026-06-25T11:00:00Z")
	if m.aiTitle != "New" || !m.lastTS.Equal(want) || m.offset != int64(len(first+rec)) {
		t.Errorf("completed: title %q ts %v offset %d", m.aiTitle, m.lastTS, m.offset)
	}
}

func TestTranscriptMetaUnterminatedValidLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	body := `{"type":"user","timestamp":"2026-06-25T08:00:00Z"}` + "\n" + `{"type":"ai-title","aiTitle":"Tail"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := mustScan(t, path, nil)
	if m.aiTitle != "Tail" {
		t.Errorf("title = %q, want %q (valid final record without newline)", m.aiTitle, "Tail")
	}
	if m.offset != int64(strings.Index(body, "\n")+1) {
		t.Errorf("offset = %d, want it left before the unterminated line", m.offset)
	}
}

func TestTranscriptMetaRewriteRescans(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"truncated": `{"type":"ai-title","aiTitle":"Short"}` + "\n",
		// Longer than the original, so size >= offset and only the tail check catches it.
		"rewritten": strings.Replace(metaFixture, "Final title", "Other title", 1) +
			`{"type":"user","timestamp":"2026-06-24T00:00:00Z"}` + "\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".jsonl")
			if err := os.WriteFile(path, []byte(metaFixture), 0o644); err != nil {
				t.Fatal(err)
			}
			prev := mustScan(t, path, nil)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			assertSameMeta(t, name, mustScan(t, path, prev), mustScan(t, path, nil))
		})
	}
}

func TestTranscriptMetaOversizedLineSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	big := `{"type":"user","timestamp":"2030-01-01T00:00:00Z","blob":"` +
		strings.Repeat("x", maxTranscriptLineBytes) + `"}` + "\n"
	head := `{"type":"ai-title","aiTitle":"Before"}` + "\n"
	after := `{"type":"ai-title","aiTitle":"After","timestamp":"2026-06-25T12:00:00Z"}` + "\n"

	// The oversized line arrives half-written first: the offset must hold at its start.
	if err := os.WriteFile(path, []byte(head+big[:len(big)/2]), 0o644); err != nil {
		t.Fatal(err)
	}
	m := mustScan(t, path, nil)
	if m.offset != int64(len(head)) || m.aiTitle != "Before" {
		t.Fatalf("partial oversized: offset %d title %q", m.offset, m.aiTitle)
	}

	appendFile(t, path, big[len(big)/2:]+after)
	m = mustScan(t, path, m)
	want, _ := time.Parse(time.RFC3339, "2026-06-25T12:00:00Z")
	if m.aiTitle != "After" || !m.lastTS.Equal(want) {
		t.Errorf("after oversized: title %q ts %v, want %q %v", m.aiTitle, m.lastTS, "After", want)
	}
	if m.offset != int64(len(head+big+after)) {
		t.Errorf("offset = %d, want %d (stalled at the long line)", m.offset, len(head+big+after))
	}
	assertSameMeta(t, "full", m, mustScan(t, path, nil))
}

// TestTranscriptMetaReadsOnlyDelta plants a record inside the already-scanned
// prefix; an incremental scan must not see it, proving only the delta is read.
func TestTranscriptMetaReadsOnlyDelta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	filler := `{"type":"assistant","timestamp":"2026-06-25T08:00:00Z","pad":"` + strings.Repeat("p", 200) + `"}` + "\n"
	body := strings.Repeat(filler, 10_000)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := mustScan(t, path, nil)

	sneaky := `{"type":"ai-title","aiTitle":"Sneaky","timestamp":"2099-01-01T00:00:00Z"}`
	sneaky += strings.Repeat(" ", len(filler)-1-len(sneaky)) + "\n"
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(sneaky), int64(len(filler)*10)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	appendFile(t, path, `{"type":"ai-title","aiTitle":"Appended","timestamp":"2026-06-25T09:00:00Z"}`+"\n")

	m := mustScan(t, path, prev)
	if m.aiTitle != "Appended" || m.lastTS.Year() != 2026 {
		t.Errorf("incremental scan re-read the prefix: title %q ts %v", m.aiTitle, m.lastTS)
	}
	if full := mustScan(t, path, nil); full.lastTS.Year() != 2099 {
		t.Fatalf("sanity: full scan should see the planted record, got %v", full.lastTS)
	}
}

func TestCachedTranscriptMetaConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(metaFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(Options{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if i == 0 {
					f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Error(err)
						return
					}
					fmt.Fprintf(f, `{"type":"ai-title","aiTitle":"T%d"}`+"\n", j)
					f.Close()
				}
				if info, err := os.Stat(path); err == nil {
					h.cachedTranscriptMeta("s", path, info)
				}
			}
		}(i)
	}
	wg.Wait()
	if _, title := h.cachedTranscriptMeta("s", path, statOrFatal(t, path)); title != "T19" {
		t.Errorf("title = %q, want %q", title, "T19")
	}
}

func benchmarkTranscriptMeta(b *testing.B, incremental bool) {
	path := filepath.Join(b.TempDir(), "s.jsonl")
	line := `{"type":"assistant","timestamp":"2026-06-25T08:00:00Z","message":{"content":"` + strings.Repeat("y", 1000) + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(line, 32_000)), 0o644); err != nil {
		b.Fatal(err)
	}
	prev := mustScan(b, path, nil)
	for b.Loop() {
		appendFile(b, path, line)
		if incremental {
			prev = mustScan(b, path, prev)
		} else {
			mustScan(b, path, nil)
		}
	}
}

// BenchmarkTranscriptMetaAppend vs ...Full shows an append to a ~32MB file
// costs a delta read rather than a full pass.
func BenchmarkTranscriptMetaAppend(b *testing.B) { benchmarkTranscriptMeta(b, true) }
func BenchmarkTranscriptMetaFull(b *testing.B)   { benchmarkTranscriptMeta(b, false) }
