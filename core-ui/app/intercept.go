package app

import (
	"fmt"
	"slices"
	"strings"
)

// Intercept records how a screen presents when it is reached by a soft navigation from a declared
// origin route.
type Intercept struct {
	// From is the registered route PATTERN the overlay presentation is allowed from, "/products", not
	// "/products?page=2".
	From string
	// AlsoFrom names more route patterns the overlay is allowed from: a create screen opens over its
	// list and over each related record whose Related tab adds to it.
	AlsoFrom []string
	// As is the overlay presentation: ScreenDrawer or ScreenSheet.
	As ScreenType
}

func (ix *Intercept) allows(pattern string) bool {
	return pattern == ix.From || slices.Contains(ix.AlsoFrom, pattern)
}

// ScreenOption adjusts a screen at registration time.
type ScreenOption func(*Screen)

// InterceptFrom presents the screen as an overlay on soft navigation from the from route.
func InterceptFrom(from string, as ScreenType, also ...string) ScreenOption {
	intercept := normalizeIntercept(&Intercept{From: from, AlsoFrom: also, As: as})
	return func(s *Screen) {
		s.Intercept = &Intercept{From: intercept.From, AlsoFrom: slices.Clone(intercept.AlsoFrom), As: as}
	}
}

func normalizeIntercept(intercept *Intercept) *Intercept {
	patterns := append([]string{intercept.From}, intercept.AlsoFrom...)
	for index, p := range patterns {
		if p == "" || p[0] != '/' {
			panic(fmt.Sprintf("app: InterceptFrom needs an absolute route pattern to intercept from, got %q", p))
		}
		patterns[index] = canonicalRoutePattern(p)
	}
	if intercept.As != ScreenDrawer && intercept.As != ScreenSheet {
		panic(fmt.Sprintf("app: InterceptFrom presentation must be ScreenDrawer or ScreenSheet, got %v", intercept.As))
	}
	return &Intercept{From: patterns[0], AlsoFrom: patterns[1:], As: intercept.As}
}

// InterceptFor reports how the screen at target should present when the navigation started at origin.
func (r *Router) InterceptFor(target, origin string) (*Intercept, bool) {
	screen, _, ok := r.Resolve(target)
	if !ok || screen.Intercept == nil {
		return nil, false
	}
	if i := strings.IndexAny(origin, "?#"); i >= 0 {
		origin = origin[:i]
	}
	originScreen, _, ok := r.Resolve(origin)
	if !ok {
		return nil, false
	}
	if !screen.Intercept.allows(originScreen.Path) {
		return nil, false
	}
	return screen.Intercept, true
}
