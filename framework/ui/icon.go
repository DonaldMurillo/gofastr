package ui

import (
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Icon ───────────────────────────────────────────────────────────
//
// Inline SVG icon primitive backed by a registry. Components currently
// reference icons by ad-hoc inline SVG strings (banner.go, carousel.go,
// backtotop.go, …); this primitive consolidates them so themes can
// override and apps can extend without duplicating wrapper boilerplate.
//
// Icons render as a single <svg> with:
//   - viewBox="0 0 24 24" (fixed grid; resize with the Size config)
//   - fill="none" stroke="currentColor" stroke-width="2"
//   - stroke-linecap="round" stroke-linejoin="round"
//
// Registered icon bodies should be the inner SVG markup (paths, lines,
// circles…) — not the <svg> wrapper. The wrapper is emitted by Icon().

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
}

// Icon renders the registered icon with the given name. Returns empty
// markup for unknown names — callers can guard with IconRegistered().
func Icon(name string, cfg IconConfig) render.HTML {
	iconRegistryMu.RLock()
	body, ok := iconRegistry[name]
	iconRegistryMu.RUnlock()
	if !ok {
		return render.HTML("")
	}

	size := cfg.Size
	if size == "" {
		size = "20"
	}
	cls := "ui-icon"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := map[string]string{
		"class":           cls,
		"xmlns":           "http://www.w3.org/2000/svg",
		"width":           size,
		"height":          size,
		"viewBox":         "0 0 24 24",
		"fill":            "none",
		"stroke":          "currentColor",
		"stroke-width":    "2",
		"stroke-linecap":  "round",
		"stroke-linejoin": "round",
	}
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
	iconRegistryMu.RLock()
	_, ok := iconRegistry[name]
	iconRegistryMu.RUnlock()
	return ok
}

// RegisterIcon adds a named icon to the registry. The body should be
// inner SVG markup (paths, lines, circles, etc.) without the outer
// <svg> wrapper. Re-registering the same name replaces the existing
// body. Safe for concurrent use.
func RegisterIcon(name, body string) {
	iconRegistryMu.Lock()
	defer iconRegistryMu.Unlock()
	iconRegistry[name] = strings.TrimSpace(body)
}

var (
	iconRegistryMu sync.RWMutex
	iconRegistry   = map[string]string{
		// Common chevrons used by Carousel, NotificationBell, Menu.
		"chevron-up":    `<polyline points="18 15 12 9 6 15"/>`,
		"chevron-down":  `<polyline points="6 9 12 15 18 9"/>`,
		"chevron-left":  `<polyline points="15 18 9 12 15 6"/>`,
		"chevron-right": `<polyline points="9 18 15 12 9 6"/>`,
		// Close (×) — used in Banner dismiss, Toast, Modal.
		"close": `<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>`,
		// Check — confirmations, completed steps.
		"check": `<polyline points="20 6 9 17 4 12"/>`,
		// Status family — Banner variants.
		"info":    `<circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>`,
		"warning": `<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>`,
		"danger":  `<circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>`,
		"success": `<polyline points="20 6 9 17 4 12"/>`,
	}
)
