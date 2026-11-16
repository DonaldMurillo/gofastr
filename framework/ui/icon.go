package ui

import (
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

// IconConfig configures an icon render.
type IconConfig struct {
	Size string // width and height as any CSS length; default "20"
	// AriaLabel names the icon as role="img"; without it the icon is aria-hidden.
	AriaLabel string
	ID        string
	Class     string
	// ExtraAttrs reach the <svg>; class, id, data-cui-* and the drawing and
	// accessibility attributes are dropped.
	ExtraAttrs html.Attrs
}

// Icon renders the named icon as an inline <svg> on the 24 by 24 grid,
// or empty markup for an unknown name.
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

// IconRegistered reports whether Icon draws the name.
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

// RegisterIcon registers trusted inner SVG markup under an exact name,
// replacing any earlier body; it is safe for concurrent use.
func RegisterIcon(name, body string) {
	iconRegistryMu.Lock()
	defer iconRegistryMu.Unlock()
	iconRegistry[name] = strings.TrimSpace(body)
}

var (
	iconRegistryMu sync.RWMutex
	iconRegistry   = map[string]string{}
)
