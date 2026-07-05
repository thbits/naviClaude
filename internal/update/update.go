// Package update checks whether a newer naviClaude release is available on
// GitHub. It is deliberately self-contained and UI-agnostic: it performs a
// single cached HTTP query against the GitHub Releases API and reports the
// latest version when it is newer than the running build.
//
// Every failure path (no network, timeout, non-200, unparseable JSON or
// version, or a "dev" build) is silent: Check returns ("", false) so callers
// can simply show nothing.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// latestReleaseURL is the GitHub API endpoint for the newest published release.
const latestReleaseURL = "https://api.github.com/repos/thbits/naviClaude/releases/latest"

// cacheTTL is how long a successful check result is reused before hitting the
// network.
const cacheTTL = 6 * time.Hour

// negativeCacheTTL is how long a failed check (no tag) is remembered before
// retrying. It is shorter than cacheTTL so an offline launch doesn't suppress
// the check for the rest of the day, but long enough that a burst of launches
// doesn't re-fire a doomed request every time.
const negativeCacheTTL = 30 * time.Minute

// httpTimeout bounds the network call so startup is never blocked for long.
const httpTimeout = 3 * time.Second

// cacheEntry is the on-disk cache format.
type cacheEntry struct {
	CheckedAt time.Time `json:"checked_at"`
	LatestTag string    `json:"latest_tag"`
}

// Check reports the latest release version and whether it is newer than
// current. It returns ("", false) when current is a dev build, on any error,
// or when current is already at or ahead of the latest release.
//
// A cache file (6h TTL) avoids repeated network calls across launches.
func Check(ctx context.Context, current string) (latest string, available bool) {
	if !isReleaseVersion(current) {
		return "", false
	}

	tag, ok := latestTag(ctx)
	if !ok {
		return "", false
	}

	if compareSemver(tag, current) > 0 {
		return normalize(tag), true
	}
	return "", false
}

// latestTag returns the latest release tag, using the cache when it is still
// fresh and otherwise fetching from GitHub (and refreshing the cache). It
// returns ok=false when no tag can be determined.
func latestTag(ctx context.Context) (tag string, ok bool) {
	if entry, err := readCache(); err == nil {
		// A failed check (empty tag) is cached briefly; a successful one for the
		// full TTL. Guard against a future CheckedAt (clock skew, restored VM
		// snapshot): a negative age must count as stale, not perpetually fresh.
		ttl := cacheTTL
		if entry.LatestTag == "" {
			ttl = negativeCacheTTL
		}
		if age := time.Since(entry.CheckedAt); age >= 0 && age < ttl {
			if entry.LatestTag == "" {
				return "", false
			}
			return entry.LatestTag, true
		}
	}

	tag, ok = fetchLatestTag(ctx)
	if !ok {
		// Cache the failure so repeated launches don't each pay a doomed network
		// attempt. Best-effort; ignore write errors.
		_ = writeCache(cacheEntry{CheckedAt: time.Now(), LatestTag: ""})
		return "", false
	}
	// Best-effort cache write; ignore errors.
	_ = writeCache(cacheEntry{CheckedAt: time.Now(), LatestTag: tag})
	return tag, true
}

// fetchLatestTag performs the GitHub API request and extracts tag_name.
func fetchLatestTag(ctx context.Context) (tag string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL(), nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", false
	}
	if payload.TagName == "" {
		return "", false
	}
	return payload.TagName, true
}

// releaseURL returns the API endpoint, allowing tests to override the target
// via the naviclaude_UPDATE_URL environment variable.
func releaseURL() string {
	if u := os.Getenv("NAVICLAUDE_UPDATE_URL"); u != "" {
		return u
	}
	return latestReleaseURL
}

// isReleaseVersion reports whether v looks like a real release version rather
// than a local/dev build.
func isReleaseVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return false
	}
	_, _, _, _, ok := parseSemver(v)
	return ok
}

// normalize returns the tag with any leading "v" stripped for display.
func normalize(tag string) string {
	return strings.TrimPrefix(strings.TrimSpace(tag), "v")
}

// compareSemver returns >0 if a is newer than b, 0 if equal, <0 if older. A
// version that fails to parse sorts as older (returns a negative result when
// compared against a valid version), which keeps Check conservative.
func compareSemver(a, b string) int {
	amaj, amin, apat, apre, aok := parseSemver(a)
	bmaj, bmin, bpat, bpre, bok := parseSemver(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	if amaj != bmaj {
		return amaj - bmaj
	}
	if amin != bmin {
		return amin - bmin
	}
	if apat != bpat {
		return apat - bpat
	}
	return comparePreRelease(apre, bpre)
}

// comparePreRelease orders pre-release identifiers per semver precedence: a
// version WITHOUT a pre-release is newer than the same core version WITH one
// (1.2.3 > 1.2.3-rc1), and two pre-releases compare field-by-field on their
// dot-separated identifiers.
func comparePreRelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1 // a is the final release, b is a pre-release
	}
	if b == "" {
		return -1
	}
	aIDs := strings.Split(a, ".")
	bIDs := strings.Split(b, ".")
	for i := 0; i < len(aIDs) && i < len(bIDs); i++ {
		if c := comparePreReleaseID(aIDs[i], bIDs[i]); c != 0 {
			return c
		}
	}
	// All shared identifiers equal: the version with more identifiers is newer.
	return len(aIDs) - len(bIDs)
}

// comparePreReleaseID compares a single pre-release identifier. Numeric
// identifiers compare numerically and always rank lower than alphanumeric ones.
func comparePreReleaseID(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return an - bn
	case aErr == nil:
		return -1 // numeric identifiers have lower precedence than alphanumeric
	case bErr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// parseSemver parses "v1.2.3" or "1.2.3", optionally with a pre-release suffix
// ("1.2.3-rc1") and/or build metadata ("1.2.3+build9"). It returns the numeric
// triplet plus the pre-release identifier (empty when absent); build metadata
// is discarded since semver excludes it from precedence. ok is false when the
// major/minor/patch triplet cannot be read.
func parseSemver(v string) (major, minor, patch int, pre string, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return 0, 0, 0, "", false
	}
	// Build metadata ("+...") is ignored for precedence; strip it first.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	// A pre-release ("-...") is retained for precedence comparison.
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, "", false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, "", false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, "", false
	}
	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, "", false
	}
	return major, minor, patch, pre, true
}

// cachePath returns the path to the update-check cache file, honoring
// NAVICLAUDE_CACHE_DIR for tests.
func cachePath() (string, error) {
	dir := os.Getenv("NAVICLAUDE_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "naviclaude")
	}
	return filepath.Join(dir, "update-check.json"), nil
}

func readCache() (cacheEntry, error) {
	var entry cacheEntry
	path, err := cachePath()
	if err != nil {
		return entry, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return entry, fmt.Errorf("parse cache: %w", err)
	}
	return entry, nil
}

func writeCache(entry cacheEntry) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	// Write atomically (temp file + rename) so a concurrent launch can never
	// read a half-written cache file.
	tmp, err := os.CreateTemp(dir, "update-check-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
