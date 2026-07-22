package session

import (
	"path/filepath"
	"testing"
)

// assistantEditBlock builds an assistant record whose message contains a single
// tool_use block for the given tool and input map.
func assistantEditBlock(tool string, input map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "tool_use", "name": tool, "input": input},
			},
		},
	}
}

func findFile(files []ChangedFile, path string) (ChangedFile, bool) {
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	return ChangedFile{}, false
}

func TestLoadChangedFiles(t *testing.T) {
	t.Run("collects edited files in first-seen order, deduped", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "session.jsonl")

		records := []map[string]interface{}{
			{"type": "user", "message": map[string]interface{}{"content": "go"}},
			assistantEditBlock("Edit", map[string]interface{}{"file_path": "/repo/a.go", "old_string": "x", "new_string": "y"}),
			assistantEditBlock("Write", map[string]interface{}{"file_path": "/repo/b.go", "content": "l1\nl2\n"}),
			assistantEditBlock("Edit", map[string]interface{}{"file_path": "/repo/a.go", "old_string": "p", "new_string": "q"}),
			assistantEditBlock("NotebookEdit", map[string]interface{}{"notebook_path": "/repo/nb.ipynb", "new_source": "cell1\ncell2\ncell3"}),
		}
		writeJSONL(t, filePath, records)

		files, err := LoadChangedFiles(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantOrder := []string{"/repo/a.go", "/repo/b.go", "/repo/nb.ipynb"}
		if len(files) != len(wantOrder) {
			t.Fatalf("got %d files %v, want %d", len(files), files, len(wantOrder))
		}
		for i, p := range wantOrder {
			if files[i].Path != p {
				t.Errorf("files[%d].Path = %q, want %q", i, files[i].Path, p)
			}
		}
	})

	t.Run("accumulates added/removed counts across edits", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "session.jsonl")

		records := []map[string]interface{}{
			// Replace one line with two: +2 -1.
			assistantEditBlock("Edit", map[string]interface{}{
				"file_path": "/repo/a.go", "old_string": "old line", "new_string": "new one\nnew two",
			}),
			// Replace two lines with one: +1 -2. Running total for a.go: +3 -3.
			assistantEditBlock("Edit", map[string]interface{}{
				"file_path": "/repo/a.go", "old_string": "gone one\ngone two", "new_string": "kept",
			}),
		}
		writeJSONL(t, filePath, records)

		files, err := LoadChangedFiles(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, ok := findFile(files, "/repo/a.go")
		if !ok {
			t.Fatalf("a.go not found in %v", files)
		}
		if a.Added != 3 || a.Removed != 3 {
			t.Errorf("a.go = +%d -%d, want +3 -3", a.Added, a.Removed)
		}
	})

	t.Run("lineDiff uses common lines", func(t *testing.T) {
		// old and new share a middle line; only the changed lines count.
		added, removed := lineDiff("a\nb\nc", "a\nB\nc")
		if added != 1 || removed != 1 {
			t.Errorf("lineDiff = +%d -%d, want +1 -1", added, removed)
		}
	})

	t.Run("ignores non-edit tools and read-only records", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "session.jsonl")

		records := []map[string]interface{}{
			assistantEditBlock("Read", map[string]interface{}{"file_path": "/repo/read-only.go"}),
			assistantEditBlock("Bash", map[string]interface{}{"command": "ls"}),
			{"type": "user", "message": map[string]interface{}{"content": "hi"}},
			assistantEditBlock("Edit", map[string]interface{}{"file_path": "/repo/real.go", "old_string": "a", "new_string": "b"}),
		}
		writeJSONL(t, filePath, records)

		files, err := LoadChangedFiles(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 || files[0].Path != "/repo/real.go" {
			t.Errorf("got %v, want [/repo/real.go]", files)
		}
	})

	t.Run("skips meta and sidechain records", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "session.jsonl")

		meta := assistantEditBlock("Edit", map[string]interface{}{"file_path": "/repo/meta.go", "old_string": "a", "new_string": "b"})
		meta["isMeta"] = true
		side := assistantEditBlock("Edit", map[string]interface{}{"file_path": "/repo/subagent.go", "old_string": "a", "new_string": "b"})
		side["isSidechain"] = true

		records := []map[string]interface{}{
			meta,
			side,
			assistantEditBlock("Edit", map[string]interface{}{"file_path": "/repo/main.go", "old_string": "a", "new_string": "b"}),
		}
		writeJSONL(t, filePath, records)

		files, err := LoadChangedFiles(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 || files[0].Path != "/repo/main.go" {
			t.Errorf("got %v, want [/repo/main.go]", files)
		}
	})

	t.Run("empty file yields no files and no error", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "empty.jsonl")
		writeJSONL(t, filePath, nil)

		files, err := LoadChangedFiles(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("got %v, want empty", files)
		}
	})

	t.Run("missing file returns an error", func(t *testing.T) {
		if _, err := LoadChangedFiles("/nonexistent/session.jsonl"); err == nil {
			t.Error("expected an error for a missing file, got nil")
		}
	})
}
