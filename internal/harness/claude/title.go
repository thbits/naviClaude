package claude

// derivedNameSource marks Claude's auto-generated placeholder name; any other
// nameSource is a real name that beats the transcript's ai-title.
const derivedNameSource = "derived"

// resolveTitle prefers a real (non-derived) meta name, then aiTitle, then the
// placeholder; "" lets the sidebar fall back to Slug/ProjectName.
func resolveTitle(metaName, nameSource, aiTitle string) string {
	if metaName != "" && nameSource != derivedNameSource {
		return metaName
	}
	if aiTitle != "" {
		return aiTitle
	}
	return metaName
}
