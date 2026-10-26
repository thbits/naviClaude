package ui

import "github.com/thbits/naviClaude/internal/styles"

// Memo caches one render, rerunning it only when its comparable key or the
// theme changes; string fields compare by content, far cheaper than a lipgloss pass.
type Memo[K comparable] struct {
	ok  bool
	gen uint64
	key K
	out string
}

// Get returns the cached render for key, or runs fn and caches it.
func (c *Memo[K]) Get(key K, fn func() string) string {
	if c == nil {
		return fn()
	}
	gen := styles.Generation()
	if c.ok && c.gen == gen && c.key == key {
		return c.out
	}
	c.key, c.gen, c.out, c.ok = key, gen, fn(), true
	return c.out
}
