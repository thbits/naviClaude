package attention

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookMarker tags the hook commands naviClaude owns in settings.json.
const hookMarker = "# naviclaude-hook"

// hookEvents are the Claude Code events naviClaude registers, with matchers.
var hookEvents = []struct{ Event, Matcher string }{
	{"PermissionRequest", ""},
	{"Notification", "permission_prompt|elicitation_dialog|elicitation_url_dialog|agent_needs_input"},
	{"PreToolUse", "AskUserQuestion|ExitPlanMode"},
	{"Stop", ""},
	{"StopFailure", ""},
	{"UserPromptSubmit", ""},
	{"SessionEnd", ""},
}

// SettingsPath is the user-level Claude Code settings file.
func SettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// HookCommand is the shell command registered for every event.
func HookCommand(exe string) string {
	return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' hook " + hookMarker
}

// InstallHooks registers (or re-points) naviClaude's hooks, keeping every other
// setting and its key order. The previous file is backed up.
func InstallHooks(path, exe string) error {
	return editHooks(path, func(event string, groups []json.RawMessage) ([]json.RawMessage, error) {
		for _, h := range hookEvents {
			if h.Event != event {
				continue
			}
			hook, err := marshalOrdered(map[string]any{"type": "command", "command": HookCommand(exe), "async": true}, "type", "command", "async")
			if err != nil {
				return nil, err
			}
			g := map[string]any{"hooks": []json.RawMessage{hook}}
			if h.Matcher != "" {
				g["matcher"] = h.Matcher
			}
			raw, err := marshalOrdered(g, "matcher", "hooks")
			if err != nil {
				return nil, err
			}
			return append(groups, raw), nil
		}
		return groups, nil
	})
}

// UninstallHooks removes naviClaude's entries from the settings file.
func UninstallHooks(path string) error {
	return editHooks(path, func(_ string, groups []json.RawMessage) ([]json.RawMessage, error) {
		return groups, nil
	})
}

// InstalledHooks lists the events that currently carry a naviClaude hook, and
// the command registered for them.
func InstalledHooks(path string) (events []string, command string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, "", err
	}
	for _, h := range hookEvents {
		for _, g := range s.Hooks[h.Event] {
			for _, c := range g.Hooks {
				if strings.Contains(c.Command, hookMarker) {
					events = append(events, h.Event)
					command = c.Command
				}
			}
		}
	}
	return events, command, nil
}

// editHooks strips naviClaude's groups from every event, lets add append new
// ones, and writes the file back through any symlink, only if a group changed.
func editHooks(path string, add func(event string, groups []json.RawMessage) ([]json.RawMessage, error)) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = []byte("{}")
	} else if err != nil {
		return err
	}
	root, err := parseObject(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	hooks := &object{}
	if raw, ok := root.get("hooks"); ok {
		if hooks, err = parseObject(raw); err != nil {
			return fmt.Errorf("%s: hooks: %w", path, err)
		}
	}
	changed := false
	events := append([]string{}, hooks.keys...)
	for _, h := range hookEvents {
		if _, ok := hooks.get(h.Event); !ok {
			events = append(events, h.Event)
		}
	}
	for _, event := range events {
		var groups []json.RawMessage
		if raw, ok := hooks.get(event); ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return fmt.Errorf("%s: hooks.%s: %w", path, event, err)
			}
		}
		kept := groups[:0]
		for _, g := range groups {
			if !bytes.Contains(g, []byte(hookMarker)) {
				kept = append(kept, g)
			}
		}
		_, existed := hooks.get(event)
		out, err := add(event, kept)
		if err != nil {
			return err
		}
		changed = changed || len(kept) != len(groups) || len(out) != len(kept)
		switch {
		case len(out) == 0 && !existed:
		case len(out) == 0 && len(groups) > 0 && len(kept) == 0:
			hooks.del(event) // only naviClaude used this event
		default:
			// Joined by hand: json.Marshal would HTML-escape the user's commands.
			parts := make([][]byte, len(out))
			for i, g := range out {
				parts[i] = g
			}
			hooks.set(event, json.RawMessage("["+string(bytes.Join(parts, []byte(",")))+"]"))
		}
	}
	if !changed {
		return nil
	}
	hooksRaw, err := hooks.marshal()
	if err != nil {
		return err
	}
	root.set("hooks", hooksRaw)
	rootRaw, err := root.marshal()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, rootRaw, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	if bytes.Equal(buf.Bytes(), data) {
		return nil
	}
	if err := backup(data); err != nil {
		return fmt.Errorf("backup settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// WriteFile follows a symlinked settings.json (e.g. stow) instead of replacing it.
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func backup(data []byte) error {
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.json.bak"), data, 0o644)
}

// object is a JSON object that keeps its key order and raw values.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseObject(data []byte) (*object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	o := &object{vals: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.set(key, raw)
	}
	return o, nil
}

func (o *object) get(k string) (json.RawMessage, bool) { v, ok := o.vals[k]; return v, ok }

func (o *object) set(k string, v json.RawMessage) {
	if o.vals == nil {
		o.vals = map[string]json.RawMessage{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) del(k string) {
	delete(o.vals, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o *object) marshal() (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		var c bytes.Buffer
		if err := json.Compact(&c, o.vals[k]); err != nil {
			return nil, err
		}
		buf.Write(c.Bytes())
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalOrdered encodes m with the given keys first, in order.
func marshalOrdered(m map[string]any, order ...string) (json.RawMessage, error) {
	o := &object{}
	for _, k := range order {
		v, ok := m[k]
		if !ok {
			continue
		}
		raw, err := marshalNoEscape(v)
		if err != nil {
			return nil, err
		}
		o.set(k, raw)
	}
	return o.marshal()
}

// marshalNoEscape is json.Marshal without HTML-escaping "&", "<" and ">", so
// shell commands stay readable in settings.json.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
