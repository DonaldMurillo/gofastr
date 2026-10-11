package app

import (
	"fmt"
	"slices"
	"strings"
)

// Intercepting routes, a detail screen that presents as an overlay when
// you reach it from inside the app, and as its own full page when you
// land on it directly.
//
// The list→detail case this serves: clicking a product in a list should
// slide the product over the list, but the product URL must still be a
// real, shareable, indexable page. Both are the SAME route. Only the
// presentation differs, and only for a soft navigation that started on a
// declared origin.
//
// This is not the same tool as a widget deep link. Use a widget deep
// link when the overlay is the ONLY way to see that content and the URL
// stays on the list (`/users?modal=user-edit&user_id=42`). Use an
// intercept when the detail has its own canonical page that must render
// standalone, the deep link is the canonical render, and the overlay is
// a presentation optimization layered on top of it.
//
// Scoping to a declared origin, rather than "any soft navigation", is
// deliberate: it keeps behavior predictable and reviewable. A screen
// says which list it overlays, so nothing else can turn it into a
// drawer by accident.
//
// Intercepts stack: a screen reached through an open overlay from a
// layer whose pattern it names opens as another overlay over it (the
// runtime caps the stack at four and refuses a fifth with a toast).
// The deep link of every layer stays the canonical full page.

// Intercept records how a screen presents when it is reached by a soft
// navigation from a declared origin route.
type Intercept struct {
	// From is the registered route PATTERN the overlay presentation is
	// allowed from, "/products", not "/products?page=2". Arriving from
	// anywhere else renders the full page.
	From string
	// AlsoFrom names more route patterns the overlay is allowed from: a
	// create screen opens over its list and over each related record
	// whose Related tab adds to it. Each is a pattern like From.
	AlsoFrom []string
	// As is the overlay presentation: ScreenDrawer or ScreenSheet.
	As ScreenType
}

// allows reports whether the overlay may open over the screen
// registered at pattern.
func (ix *Intercept) allows(pattern string) bool {
	return pattern == ix.From || slices.Contains(ix.AlsoFrom, pattern)
}

// ScreenOption adjusts a screen at registration time. Options are
// applied after the screen's own interfaces are read, so an option
// always wins over what the component declares.
type ScreenOption func(*Screen)

// InterceptFrom marks a screen as intercepting: navigating to it from
// within the `from` route presents it as an overlay, while a hard load,
// a refresh, an external link, or a soft navigation from anywhere else
// renders the ordinary full page.
//
//	site.Register("/products/:id", &ProductScreen{}, nil,
//	    app.InterceptFrom("/products", app.ScreenDrawer))
//
// The screen stays a normal page registration, SSR-first holds and the
// deep link remains the canonical render. Only the soft-navigation path
// changes. also names more origin patterns (Intercept.AlsoFrom).
//
// Panics at registration (never at request time) when from or an also
// pattern is not an absolute path, or as is not an overlay type,
// matching the boot-time failure style of the unknown-variant component
// panics.
func InterceptFrom(from string, as ScreenType, also ...string) ScreenOption {
	for _, p := range append([]string{from}, also...) {
		if p == "" || p[0] != '/' {
			panic(fmt.Sprintf("app: InterceptFrom needs an absolute route pattern to intercept from, got %q", p))
		}
	}
	if as != ScreenDrawer && as != ScreenSheet {
		panic(fmt.Sprintf("app: InterceptFrom presentation must be ScreenDrawer or ScreenSheet, got %v", as))
	}
	return func(s *Screen) { s.Intercept = &Intercept{From: from, AlsoFrom: also, As: as} }
}

// InterceptFor reports how the screen at target should present when the
// navigation started at origin.
//
// It answers with an overlay ONLY when the target screen declares an
// intercept AND origin resolves to the screen whose registered pattern
// the intercept named. Everything else, no intercept, an origin that
// matches nothing, an origin that is a different screen, reports false,
// so the caller renders the ordinary page. The failure direction matters:
// the client asks for an overlay, the server decides, and any doubt
// resolves to the canonical full render.
//
// origin is compared by RESOLVED PATTERN, not by string. "/products?
// page=2&sort=name" and "/products" are the same screen and both
// intercept; "/products/9/edit" is a different screen and does not.
//
// The origin may itself be an open intercept layer: a related record
// whose screen intercepts from the record's pattern stacks as a new
// drawer over it (at most four deep; a fifth open is refused). The
// server sees only URLs, so stacking needs no extra flag — declare the
// chain the same way as the first open.
func (r *Router) InterceptFor(target, origin string) (*Intercept, bool) {
	screen, _, ok := r.Resolve(target)
	if !ok || screen.Intercept == nil {
		return nil, false
	}
	// The origin arrives as a live browser location, so it carries the
	// list's own state, "/products?page=2&sort=name#row-9". Resolve the
	// path alone: a filtered or scrolled list is still the list, and
	// intercepting has to survive the user having paged through it.
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
