package ui

import (
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
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
		// Close (×): used in Banner dismiss, Toast, Modal.
		"close": `<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>`,
		// Menu (three bars): the trigger of a phone navigation menu.
		"menu": `<line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/>`,
		// More (three dots): an icon-only Menu trigger.
		"more": `<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`,
		// Search (magnifier): the leading mark in SearchInput.
		"search": `<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>`,
		// Check: confirmations, completed steps.
		"check": `<polyline points="20 6 9 17 4 12"/>`,
		// Status family: Banner variants.
		"info":    `<circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>`,
		"warning": `<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>`,
		"danger":  `<circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>`,
		"success": `<polyline points="20 6 9 17 4 12"/>`,
		// Navigation family: sidebar groups, dashboard cards, palette rows.
		// An entity's Display.Nav.Icon names one of these, or one an app
		// registers.
		"home":       `<path d="M3 11l9-8 9 8"/><path d="M5 10v10h14V10"/><path d="M10 20v-6h4v6"/>`,
		"user":       `<circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 4-6 8-6s8 2 8 6"/>`,
		"users":      `<circle cx="9" cy="8" r="3.5"/><path d="M2 20c0-3.5 3-5.5 7-5.5s7 2 7 5.5"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7"/><path d="M18 14.8c2.4.6 4 2.3 4 5.2"/>`,
		"file":       `<path d="M14 3H6v18h12V7z"/><polyline points="14 3 14 7 18 7"/>`,
		"receipt":    `<path d="M5 3h14v18l-3-2-2 2-2-2-2 2-2-2-3 2z"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="9" y1="12" x2="15" y2="12"/>`,
		"card":       `<rect x="2" y="5" width="20" height="14" rx="2"/><line x1="2" y1="10" x2="22" y2="10"/>`,
		"box":        `<path d="M3 7l9-4 9 4v10l-9 4-9-4z"/><polyline points="3 7 12 11 21 7"/><line x1="12" y1="11" x2="12" y2="21"/>`,
		"layers":     `<polygon points="12 3 22 8 12 13 2 8 12 3"/><polyline points="2 13 12 18 22 13"/>`,
		"activity":   `<polyline points="2 12 6 12 9 4 15 20 18 12 22 12"/>`,
		"shield":     `<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/>`,
		"key":        `<circle cx="7.5" cy="15.5" r="4.5"/><path d="M10.7 12.3L20 3"/><path d="M16 7l3 3"/>`,
		"lock":       `<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>`,
		"sliders":    `<line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/><circle cx="9" cy="6" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="7" cy="18" r="2"/>`,
		"chart":      `<line x1="4" y1="20" x2="20" y2="20"/><rect x="5" y="11" width="3" height="6"/><rect x="10.5" y="6" width="3" height="11"/><rect x="16" y="9" width="3" height="8"/>`,
		"calendar":   `<rect x="3" y="5" width="18" height="16" rx="2"/><line x1="3" y1="10" x2="21" y2="10"/><line x1="8" y1="3" x2="8" y2="7"/><line x1="16" y1="3" x2="16" y2="7"/>`,
		"clock":      `<circle cx="12" cy="12" r="9"/><polyline points="12 7 12 12 15 14"/>`,
		"plus":       `<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>`,
		"list":       `<line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="4.5" cy="6" r="1"/><circle cx="4.5" cy="12" r="1"/><circle cx="4.5" cy="18" r="1"/>`,
		"grid":       `<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>`,
		"database":   `<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>`,
		"inbox":      `<path d="M3 13l3-8h12l3 8v6H3z"/><path d="M3 13h5l1 3h6l1-3h5"/>`,
		"folder":     `<path d="M3 6a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z"/>`,
		"tag":        `<path d="M3 3h8l10 10-8 8L3 11z"/><circle cx="7.5" cy="7.5" r="1.5"/>`,
		"mail":       `<rect x="3" y="5" width="18" height="14" rx="2"/><polyline points="3 7 12 13 21 7"/>`,
		"bell":       `<path d="M6 16v-5a6 6 0 0 1 12 0v5l2 2H4z"/><path d="M10 21h4"/>`,
		"globe":      `<circle cx="12" cy="12" r="9"/><line x1="3" y1="12" x2="21" y2="12"/><path d="M12 3c2.5 2.5 3.5 5.5 3.5 9s-1 6.5-3.5 9c-2.5-2.5-3.5-5.5-3.5-9s1-6.5 3.5-9z"/>`,
		"repeat":     `<polyline points="17 2 21 6 17 10"/><path d="M3 12v-2a4 4 0 0 1 4-4h14"/><polyline points="7 22 3 18 7 14"/><path d="M21 12v2a4 4 0 0 1-4 4H3"/>`,
		"cpu":        `<rect x="6" y="6" width="12" height="12" rx="1"/><rect x="9.5" y="9.5" width="5" height="5"/><line x1="9" y1="2" x2="9" y2="6"/><line x1="15" y1="2" x2="15" y2="6"/><line x1="9" y1="18" x2="9" y2="22"/><line x1="15" y1="18" x2="15" y2="22"/><line x1="2" y1="9" x2="6" y2="9"/><line x1="2" y1="15" x2="6" y2="15"/><line x1="18" y1="9" x2="22" y2="9"/><line x1="18" y1="15" x2="22" y2="15"/>`,
		"panel-left": `<rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="3" x2="9" y2="21"/>`,
		"star":       `<polygon points="12 3 14.8 8.8 21 9.6 16.5 14 17.6 20.2 12 17.2 6.4 20.2 7.5 14 3 9.6 9.2 8.8 12 3"/>`,
	}
)
