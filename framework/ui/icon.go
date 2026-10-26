package ui

import (
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

// ─── Icon ───────────────────────────────────────────────────────────
//
// Inline SVG renderer for the vendored framework/ui/icons catalog, with
// exact-name application overrides through RegisterIcon.
//
// Icons render as a single <svg> with:
//   - viewBox="0 0 24 24" (fixed grid; resize with the Size config)
//   - fill="none" stroke="currentColor" stroke-width="2"
//   - stroke-linecap="round" stroke-linejoin="round"
//
// Registered icon bodies should be the inner SVG markup (paths, lines,
// circles…), not the <svg> wrapper. The wrapper is emitted by Icon().

// IconConfig configures an icon render.
type IconConfig struct {
	// Size sets the rendered width/height. Accepts any CSS length
	// (e.g. "20", "1.25rem"). Default: "20".
	Size string

	// AriaLabel makes the icon meaningful to assistive tech. When set,
	// the SVG renders with role="img" and aria-label="<AriaLabel>";
	// without it, the icon is aria-hidden="true" (decorative).
	AriaLabel string

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the icon's root <svg>
	// element. Keys the component owns are dropped: class and id
	// (use Class / ID), data-cui-*, and the drawing / accessibility
	// keys the component derives (xmlns, width, height, viewBox,
	// fill, stroke*, role, aria-label, aria-hidden).
	ExtraAttrs html.Attrs
}

// Icon renders the registered icon with the given name. Returns empty
// markup for unknown names. Callers can guard with IconRegistered().
func Icon(name string, cfg IconConfig) render.HTML {
	body, ok := lookupIcon(name)
	if !ok {
		return render.HTML("")
	}

	size := cfg.Size
	if size == "" {
		size = "20"
	}
	cls := "fui-icon"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"xmlns", "width", "height", "viewBox", "fill", "stroke",
		"stroke-width", "stroke-linecap", "stroke-linejoin",
		"role", "aria-label", "aria-hidden")
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["class"] = cls
	attrs["xmlns"] = "http://www.w3.org/2000/svg"
	attrs["width"] = size
	attrs["height"] = size
	attrs["viewBox"] = "0 0 24 24"
	attrs["fill"] = "none"
	attrs["stroke"] = "currentColor"
	attrs["stroke-width"] = "2"
	attrs["stroke-linecap"] = "round"
	attrs["stroke-linejoin"] = "round"
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.AriaLabel != "" {
		attrs["role"] = "img"
		attrs["aria-label"] = cfg.AriaLabel
	} else {
		attrs["aria-hidden"] = "true"
	}

	return render.Tag("svg", attrs, render.HTML(body))
}

// IconRegistered reports whether an icon with the given name is in
// the registry.
func IconRegistered(name string) bool {
	_, ok := lookupIcon(name)
	return ok
}

func lookupIcon(name string) (string, bool) {
	iconRegistryMu.RLock()
	body, ok := iconRegistry[name]
	iconRegistryMu.RUnlock()
	if ok {
		return body, true
	}
	return icons.Lookup(name)
}

// RegisterIcon adds a named icon to the registry. The body should be
// inner SVG markup (paths, lines, circles, etc.) without the outer
// <svg> wrapper. Re-registering the same name replaces the existing
// body. Safe for concurrent use.
// Body is trusted application markup, not sanitized user input. Overrides are
// exact-name: overriding "check" does not change the built-in "success" alias.
func RegisterIcon(name, body string) {
	iconRegistryMu.Lock()
	defer iconRegistryMu.Unlock()
	iconRegistry[name] = strings.TrimSpace(body)
}

var (
	iconRegistryMu sync.RWMutex
	iconRegistry   = map[string]string{}
)
