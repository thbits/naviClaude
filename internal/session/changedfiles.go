package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ChangedFile is a file the session edited, with the added/removed line counts
// accumulated across every edit the session made to it. As loaded here the
// counts are derived from the transcript (old_string vs new_string per edit) --
// a durable but approximate record of what the session changed. Callers may
// replace the counts with a live git diff (see GitDiffStats) and clear
// Estimated; when no live diff exists (e.g. the work was committed) the
// transcript counts remain as the persistent estimate.
type ChangedFile struct {
	Path      string
	Added     int
	Removed   int
	Estimated bool // true when counts are the transcript estimate, not a live git diff
}

// editToolNames is the set of tool names whose invocations mutate a file on
// disk. A tool_use block with one of these names carries the touched path in
// its input, so it is what "files changed in the session" is derived from.
var editToolNames = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// changedFilesBlock captures the fields of a single message content block
// needed to identify a file-editing tool call. It extends the metrics
// contentBlock shape with the tool name and raw input so the input is decoded
// lazily only for edit tools.
type changedFilesBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// changedFilesInput captures the fields an edit tool_use block may carry across
// the four edit tools: Edit/MultiEdit use old_string/new_string (MultiEdit
// nests them in edits), Write uses content, NotebookEdit uses new_source.
type changedFilesInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	OldString    string `json:"old_string"`
	NewString    string `json:"new_string"`
	Content      string `json:"content"`
	NewSource    string `json:"new_source"`
	Edits        []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
}

// LoadChangedFiles reads a Claude JSONL session file and returns the files the
// session edited, in first-seen order, each with the added/removed line counts
// accumulated across all of the session's edits to it. It looks for assistant
// tool_use blocks naming a file-editing tool (Edit/Write/MultiEdit/
// NotebookEdit) and extracts the touched path and its line delta.
//
// Scanning mirrors LoadMetrics: a large scanner buffer tolerates long
// transcript lines, and a mid-file scan error still yields the files collected
// so far, so only an outright open failure is reported as an error.
func LoadChangedFiles(filePath string) ([]ChangedFile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var files []ChangedFile
	index := make(map[string]int) // path -> position in files

	accumulate := func(path string, added, removed int) {
		if path == "" {
			return
		}
		if i, ok := index[path]; ok {
			files[i].Added += added
			files[i].Removed += removed
			return
		}
		index[path] = len(files)
		// Transcript-derived counts are estimates until a live git diff confirms
		// them (done by the caller).
		files = append(files, ChangedFile{Path: path, Added: added, Removed: removed, Estimated: true})
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec metricsRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		// Only assistant records issue tool calls; skip meta/sidechain records
		// so subagent edits don't pollute the selected session's list.
		if rec.Type != "assistant" || rec.IsMeta || rec.IsSidechain {
			continue
		}

		var wrapper struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rec.Message, &wrapper); err != nil {
			continue
		}

		var blocks []changedFilesBlock
		if err := json.Unmarshal(wrapper.Content, &blocks); err != nil {
			continue
		}

		for _, b := range blocks {
			if b.Type != "tool_use" || !editToolNames[b.Name] {
				continue
			}
			var in changedFilesInput
			if err := json.Unmarshal(b.Input, &in); err != nil {
				continue
			}

			switch b.Name {
			case "Edit":
				added, removed := lineDiff(in.OldString, in.NewString)
				accumulate(in.FilePath, added, removed)
			case "MultiEdit":
				var added, removed int
				for _, e := range in.Edits {
					a, r := lineDiff(e.OldString, e.NewString)
					added += a
					removed += r
				}
				accumulate(in.FilePath, added, removed)
			case "Write":
				// A Write creates or overwrites the file; the prior contents
				// are not in the transcript, so only additions are known.
				accumulate(in.FilePath, countLines(in.Content), 0)
			case "NotebookEdit":
				accumulate(in.NotebookPath, countLines(in.NewSource), 0)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return files, err
	}
	return files, nil
}

// lineDiff returns the added and removed line counts between old and new,
// computed from the longest common subsequence of their lines -- the same
// notion of "+N -M" that git and Claude Code display for a hunk.
func lineDiff(old, new string) (added, removed int) {
	o := splitLines(old)
	n := splitLines(new)
	common := lcsLen(o, n)
	return len(n) - common, len(o) - common
}

// countLines returns the number of lines in s (a trailing newline does not
// count as an extra empty line).
func countLines(s string) int {
	return len(splitLines(s))
}

// splitLines splits s into lines, treating a single trailing newline as a line
// terminator rather than introducing a trailing empty line. An empty string is
// zero lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// lcsLen returns the length of the longest common subsequence of two line
// slices via the standard O(len(a)*len(b)) dynamic-programming table. Edit
// hunks are small, so the quadratic cost is negligible.
func lcsLen(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] >= curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
