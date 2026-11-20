package app

// Preload modes a screen can declare.
const (
	// PreloadHover prefetches when a link to the route is hovered or keyboard-focused.
	PreloadHover = "hover"
	// PreloadVisible prefetches when a link to the route scrolls into the viewport.
	PreloadVisible = "visible"
	// PreloadEager prefetches at idle shortly after page load.
	PreloadEager = "eager"
)

// Preload is a ScreenOption declaring that the client may prefetch this route's content before navigation.
func Preload(mode string) ScreenOption {
	switch mode {
	case PreloadHover, PreloadVisible, PreloadEager:
	default:
		panic("app: Preload mode " + mode + " unknown (use PreloadHover, PreloadVisible, or PreloadEager)")
	}
	return func(s *Screen) { s.Preload = mode }
}
