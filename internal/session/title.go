package session

// derivedNameSource is the nameSource value Claude Code writes in
// ~/.claude/sessions/<pid>.json for the auto-generated placeholder title
// (e.g. "<project>-<hash>"). Any other value (user-set, background session)
// is a real name that should win over the transcript's ai-title.
const derivedNameSource = "derived"

// resolveTitle picks a session's display title from the two on-disk sources:
// metaName/nameSource (from the per-PID <pid>.json) and aiTitle (the latest
// "ai-title" transcript record). Precedence, highest first:
//
//  1. metaName when nameSource is a real (non-derived) source
//  2. aiTitle
//  3. metaName (the derived placeholder, possibly "")
//
// User aliases sit above all of these and are applied separately by the app
// layer (applyAliases). Returning "" leaves the sidebar to fall back to
// Slug/ProjectName.
func resolveTitle(metaName, nameSource, aiTitle string) string {
	if metaName != "" && nameSource != derivedNameSource {
		return metaName
	}
	if aiTitle != "" {
		return aiTitle
	}
	return metaName
}
