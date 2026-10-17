package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

// changedFilesMsg carries the files edited during the selected session, plus
// the transcript's modification time at load, used to gate live refreshes.
type changedFilesMsg struct {
	sessionID string
	files     []session.ChangedFile
	mtime     time.Time
}

// editorDoneMsg signals that the $EDITOR process exited. err is non-nil if the
// editor failed to launch or returned a non-zero status.
type editorDoneMsg struct{ err error }

// loadChangedFilesCmd loads a session's edited files asynchronously via its
// harness, overlaying live git diff counts.
func loadChangedFilesCmd(h harness.Harness, sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		cf, ok := harness.AsChangedFiles(h)
		if h == nil || !ok {
			return changedFilesMsg{sessionID: sess.ID}
		}
		files, _ := cf.LoadChangedFiles(sess)
		applyGitCounts(files, session.GitDiffStats(sess.CWD))
		return changedFilesMsg{sessionID: sess.ID, files: files, mtime: cf.ChangeStamp(sess)}
	}
}

// applyGitCounts overlays live git diff counts onto the transcript-derived list;
// files with no pending diff (committed, or no repo) keep the transcript estimate.
// Paths are canonicalized so symlinked roots like /var vs /private/var match git.
func applyGitCounts(files []session.ChangedFile, stats map[string]session.DiffStat) {
	if stats == nil {
		return // no repo: keep transcript estimates as-is
	}
	for i := range files {
		key := files[i].Path
		if resolved, err := filepath.EvalSymlinks(key); err == nil {
			key = resolved
		}
		if ds, ok := stats[key]; ok {
			files[i].Added = ds.Added
			files[i].Removed = ds.Removed
			files[i].Estimated = false
		}
		// else: no pending diff -- leave the transcript estimate in place.
	}
}

// openInEditorCmd opens path in $EDITOR from the file's directory; tea.ExecProcess
// hands it the real terminal, which a tmux popup can't reliably provide.
func (m Model) openInEditorCmd(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	fields := resolveEditorFields(m.cfg.Editor)
	cmd := exec.Command(fields[0], append(fields[1:], path)...)

	if isGUIEditor(fields[0]) {
		// A GUI editor has its own window, so run it in the background to keep the
		// TUI rendering; nil stdio keeps it from garbling the screen.
		return func() tea.Msg {
			return editorDoneMsg{err: cmd.Run()}
		}
	}

	// A terminal editor (vi/vim/nvim/nano/...) needs the real terminal, so
	// suspend the TUI, run it attached, and restore on exit.
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{err: err}
	})
}

// resolveEditorFields splits config `editor`, else $EDITOR, else vi into command
// and flags; the path is appended as its own argument, so no shell is involved.
func resolveEditorFields(configured string) []string {
	editor := strings.TrimSpace(configured)
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = "vi"
	}
	return strings.Fields(editor)
}

// guiEditors lists editor launcher commands that open their own window and must
// NOT take over the terminal. Matched case-insensitively on the base command.
var guiEditors = map[string]bool{
	"cursor": true, "code": true, "code-insiders": true, "codium": true,
	"vscodium": true, "subl": true, "sublime_text": true, "zed": true,
	"windsurf": true, "atom": true, "gvim": true, "mvim": true, "mate": true,
	"bbedit": true, "idea": true, "goland": true, "pycharm": true,
	"webstorm": true, "phpstorm": true, "rubymine": true, "clion": true,
	"rider": true, "fleet": true, "gedit": true, "kate": true, "xed": true,
	"open": true,
}

// isGUIEditor reports whether command names a GUI editor that should be launched
// detached rather than taking over the terminal.
func isGUIEditor(command string) bool {
	base := strings.ToLower(filepath.Base(command))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".cmd")
	return guiEditors[base]
}
