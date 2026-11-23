// Package skills embeds the Claude Code skills shipped with naviClaude.
package skills

import _ "embed"

// NaviClaude is skills/naviclaude/SKILL.md, printed by `naviclaude skill`.
//
//go:embed naviclaude/SKILL.md
var NaviClaude string
