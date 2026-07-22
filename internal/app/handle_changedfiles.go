package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// changedFilesMsg carries the files edited during the selected session.
type changedFilesMsg struct {
	sessionID string
	files     []session.ChangedFile
}

// editorDoneMsg signals that the $EDITOR process exited. err is non-nil if the
// editor failed to launch or returned a non-zero status.
type editorDoneMsg struct{ err error }

// loadChangedFilesCmd reads a session's edited files from its JSONL transcript
// asynchronously, mirroring loadMetricsCmd's transcript-path resolution.
func loadChangedFilesCmd(sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		filePath := sess.SessionFile
		if filePath == "" && sess.ID != "" && sess.CWD != "" {
			filePath = session.SessionFilePath(sess.ID, sess.CWD)
		}
		if filePath == "" {
			return changedFilesMsg{sessionID: sess.ID}
		}
		files, _ := session.LoadChangedFiles(filePath)
		applyGitCounts(files, session.GitDiffStats(sess.CWD))
		return changedFilesMsg{sessionID: sess.ID, files: files}
	}
}

// applyGitCounts overlays live git diff counts onto the transcript-derived file
// list. When a file has a pending git diff its exact net counts replace the
// transcript churn and it is marked non-estimated; when it does not (the work
// was committed/merged, or there is no repo -- stats is nil) the transcript
// counts remain as a durable estimate, so the changes stay visible after a
// commit. Matching uses the canonicalized path so symlinked roots (e.g. /var vs
// /private/var on macOS) still line up with git's output.
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

// openInEditorCmd opens path in the user's $EDITOR. It uses tea.ExecProcess,
// which suspends the TUI and hands the real terminal to the editor, then
// restores the TUI on exit -- the reliable way to run a full-screen editor like
// nvim (a tmux popup does not always get a usable pty). The editor runs in the
// file's own directory so relative navigation inside it behaves as expected.
func (m Model) openInEditorCmd(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	fields := resolveEditorFields(m.cfg.Editor)
	cmd := exec.Command(fields[0], append(fields[1:], path)...)

	if isGUIEditor(fields[0]) {
		// A GUI editor has its own window and does not need the terminal.
		// Launch it in the background so the TUI keeps rendering -- using
		// tea.ExecProcess here would hand the terminal over and blank naviClaude
		// until the editor closed. The default nil stdio connects the child to
		// the null device, so it cannot garble the TUI, and cmd.Run reaps it.
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

// resolveEditorFields resolves the editor command and splits it into command +
// flags (no file path). Resolution order: the configured value (config
// `editor`), then $EDITOR, then vi. The value may include flags (e.g.
// "code -w", "nvim -p"); the path is later appended as a separate argument
// rather than passed through a shell, so no shell quoting or injection applies.
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
