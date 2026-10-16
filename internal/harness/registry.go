package harness

import "github.com/thbits/naviClaude/internal/session"

// Registry is the ordered set of harnesses naviClaude observes. Order decides
// which harness owns a pane matched by several, and the first is the default.
type Registry struct {
	list   []Harness
	byName map[string]Harness
}

// NewRegistry builds a registry; it panics on no harnesses or duplicate names
// since both are programming errors in startup wiring.
func NewRegistry(hs ...Harness) *Registry {
	if len(hs) == 0 {
		panic("harness: registry needs at least one harness")
	}
	r := &Registry{byName: make(map[string]Harness, len(hs))}
	for _, h := range hs {
		name := h.Info().Name
		if _, dup := r.byName[name]; dup {
			panic("harness: duplicate harness " + name)
		}
		r.byName[name] = h
		r.list = append(r.list, h)
	}
	return r
}

// All returns the harnesses in registration order.
func (r *Registry) All() []Harness { return r.list }

// Get looks up a harness by name.
func (r *Registry) Get(name string) (Harness, bool) {
	h, ok := r.byName[name]
	return h, ok
}

// Default is the harness used for new sessions and unstamped sessions.
func (r *Registry) Default() Harness { return r.list[0] }

// For returns the harness owning s; an empty Harness field resolves to Default.
func (r *Registry) For(s *session.Session) (Harness, bool) {
	if s == nil {
		return nil, false
	}
	if s.Harness == "" {
		return r.Default(), true
	}
	return r.Get(s.Harness)
}
