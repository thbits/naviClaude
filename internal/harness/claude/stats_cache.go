package claude

import (
	"encoding/gob"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// transcriptCacheVersion is bumped whenever transcriptSummary or the parsing
// rules change, so stale summaries are discarded rather than trusted.
const transcriptCacheVersion = 1

type transcriptCacheFile struct {
	Version int
	Files   map[string]*transcriptSummary
}

// transcriptCache keeps per-file summaries in memory and on disk so a stats
// recompute only parses transcripts whose (size, mtime) changed.
type transcriptCache struct {
	mu     sync.Mutex
	path   string // "" keeps the cache in memory only
	files  map[string]*transcriptSummary
	loaded bool
}

var defaultTranscriptCache = &transcriptCache{path: defaultTranscriptCachePath()}

func defaultTranscriptCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "naviclaude", "transcript-stats.gob")
}

// transcriptFile is a transcript found under projects/.
type transcriptFile struct {
	path string
	main bool // a top-level session file rather than a subagent transcript
	info fs.FileInfo
}

// listTranscripts finds main sessions (projects/<slug>/<id>.jsonl) and subagent
// transcripts nested below a session dir.
func listTranscripts(claudeDir string) []transcriptFile {
	root := filepath.Join(claudeDir, "projects")
	var out []transcriptFile
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		sep := string(filepath.Separator)
		if depth < 1 || (depth > 1 && !strings.Contains(sep+rel, sep+"subagents"+sep)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, transcriptFile{path: p, main: depth == 1, info: info})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// summaries returns a summary for every transcript, rescanning only files whose
// size or mtime changed, and persists the cache when anything changed.
func (c *transcriptCache) summaries(files []transcriptFile) map[string]*transcriptSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.files = c.load()
		c.loaded = true
	}

	var stale []transcriptFile
	for _, f := range files {
		prev := c.files[f.path]
		if prev == nil || prev.Main != f.main || prev.Size != f.info.Size() || prev.ModTime != f.info.ModTime().UnixNano() {
			stale = append(stale, f)
		}
	}

	results := make([]*transcriptSummary, len(stale))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f := stale[i]
				if s, err := scanTranscript(f.path, f.main, f.info, c.files[f.path]); err == nil {
					results[i] = s
				}
			}
		}()
	}
	for i := range stale {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	changed := false
	for i, s := range results {
		if s != nil {
			c.files[stale[i].path] = s
			changed = true
		}
	}
	present := make(map[string]bool, len(files))
	out := make(map[string]*transcriptSummary, len(files))
	for _, f := range files {
		present[f.path] = true
		if s := c.files[f.path]; s != nil {
			out[f.path] = s
		}
	}
	for p := range c.files {
		if !present[p] {
			delete(c.files, p)
			changed = true
		}
	}
	if changed {
		_ = c.save()
	}
	return out
}

func (c *transcriptCache) load() map[string]*transcriptSummary {
	empty := map[string]*transcriptSummary{}
	if c.path == "" {
		return empty
	}
	f, err := os.Open(c.path)
	if err != nil {
		return empty
	}
	defer f.Close()
	var data transcriptCacheFile
	if err := gob.NewDecoder(f).Decode(&data); err != nil || data.Version != transcriptCacheVersion || data.Files == nil {
		return empty
	}
	return data.Files
}

// save writes the cache atomically so a concurrent naviClaude never reads a
// torn file.
func (c *transcriptCache) save() error {
	if c.path == "" {
		return nil
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "transcript-stats-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := gob.NewEncoder(tmp).Encode(transcriptCacheFile{Version: transcriptCacheVersion, Files: c.files}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}
