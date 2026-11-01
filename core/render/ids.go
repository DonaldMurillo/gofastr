package render

import (
	"context"
	"strconv"
	"sync"
)

// A component that derives an element id from its content (a section
// slugged from its heading, a tab strip named after its signal) gives
// two same-titled instances on one page the same id: aria-labelledby
// and a #fragment then resolve to the first. The id scope on the render
// context hands each derived id out once per render and suffixes a
// repeat: "features", then "features-2".
//
// The host installs a scope once per page render (core-ui/app's
// WithRequest does). A component rendered with no scope on its context,
// or with no context, keeps the id it derived. An explicit id is never
// rewritten; ReserveID records it so a derived id that would repeat it
// is suffixed instead.

type idScopeKey struct{}

type idScope struct {
	mu   sync.Mutex
	seen map[string]int // id -> the highest suffix handed out for it
}

// WithIDScope returns a context carrying a fresh id scope.
func WithIDScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, idScopeKey{}, &idScope{seen: map[string]int{}})
}

// ForkIDScope returns a context carrying a copy of ctx's id scope, for a
// part of the page that renders concurrently with its siblings: ids the
// page handed out before the fork stay taken, and the fork's own
// repeats are suffixed in its own order, so the bytes do not depend on
// which sibling finished first. Repeats across concurrent siblings are
// not suffixed. With no scope on ctx it returns ctx.
func ForkIDScope(ctx context.Context) context.Context {
	s := scopeOf(ctx)
	if s == nil {
		return ctx
	}
	s.mu.Lock()
	seen := make(map[string]int, len(s.seen))
	for k, v := range s.seen {
		seen[k] = v
	}
	s.mu.Unlock()
	return context.WithValue(ctx, idScopeKey{}, &idScope{seen: seen})
}

// UniqueID returns id the first time ctx's scope sees it and id-2,
// id-3, … on each repeat, skipping a suffixed id already taken. With no
// scope (or a nil ctx) or an empty id it returns id unchanged.
func UniqueID(ctx context.Context, id string) string {
	s := scopeOf(ctx)
	if s == nil || id == "" {
		return id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.seen[id]
	if n == 0 {
		s.seen[id] = 1
		return id
	}
	for {
		n++
		cand := id + "-" + strconv.Itoa(n)
		if s.seen[cand] == 0 {
			s.seen[id] = n
			s.seen[cand] = 1
			return cand
		}
	}
}

// ReserveID records an explicit id in ctx's scope without changing it,
// so a later derived id that would repeat it is suffixed.
func ReserveID(ctx context.Context, id string) {
	s := scopeOf(ctx)
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	if s.seen[id] == 0 {
		s.seen[id] = 1
	}
	s.mu.Unlock()
}

func scopeOf(ctx context.Context) *idScope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(idScopeKey{}).(*idScope)
	return s
}
