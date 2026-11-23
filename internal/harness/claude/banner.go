package claude

import "encoding/json"

// SessionTitle names a session the way the sidebar does before aliases: a real
// name, else the transcript's ai-title, else Claude's placeholder.
func SessionTitle(sessionID, transcriptPath string) string {
	var name, source, aiTitle string
	if m, ok := readSessionMetadataByID(sessionID); ok {
		name, source = m.Name, m.NameSource
	}
	if (name == "" || source == derivedNameSource) && transcriptPath != "" {
		if meta, err := scanTranscriptMeta(transcriptPath, nil); err == nil {
			aiTitle = meta.aiTitle
		}
	}
	return resolveTitle(name, source, aiTitle)
}

// ToolSummary returns a tool's display name and the most telling part of its input.
func ToolSummary(name string, input json.RawMessage) (string, string) {
	return toolSummary(name, input)
}
