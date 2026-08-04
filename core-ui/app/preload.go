package app

// Preload modes a screen can declare. The client prefetches the route's
// partial HTML into a short-lived cache so the eventual click paints
// without a round-trip. Prefetching is GET-only, skips session side
// effects, and never caches overlay (intercepted) renders.
const (
	// PreloadHover prefetches when a link to the route is hovered or
	// keyboard-focused. The right default for primary nav links.
	PreloadHover = "hover"
	// PreloadVisible prefetches when a link to the route scrolls into the
	// viewport. For link lists where hover intent is too late (mobile).
	PreloadVisible = "visible"
	// PreloadEager prefetches at idle shortly after page load. Reserve for
	// the one or two routes almost every session visits next.
	PreloadEager = "eager"
)

// Preload is a ScreenOption declaring that the client may prefetch this
// route's content before navigation. mode is one of PreloadHover,
// PreloadVisible, or PreloadEager; any other value panics at registration
// (a typo would otherwise silently disable prefetch).
//
//	site.Register("/pricing", &PricingScreen{}, layout, app.Preload(app.PreloadHover))
func Preload(mode string) ScreenOption {
	switch mode {
	case PreloadHover, PreloadVisible, PreloadEager:
	default:
		panic("app: Preload mode " + mode + " unknown (use PreloadHover, PreloadVisible, or PreloadEager)")
	}
	return func(s *Screen) { s.Preload = mode }
}
