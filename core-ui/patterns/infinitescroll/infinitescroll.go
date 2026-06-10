package infinitescroll

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. The component's CSS
// auto-loads on first appearance via the runtime's data-fui-comp
// scanner — no app-side wiring required. Apps that want to override
// the defaults do so via theme tokens (--color-*, --spacing-*,
// --radii-*); the scoped selectors below ensure overrides cascade.
var Style = registry.RegisterStyle("infinitescroll", styleFn)

// Render renders the SSR shell: a feed container holding the initial
// items, a hidden sentinel for the IntersectionObserver, and a
// <noscript> fallback "Load more" form. The runtime wires the
// sentinel + cursor on first paint.
func Render(cfg Config) render.HTML {
	if cfg.RPCPath == "" {
		panic("infinitescroll: Render requires RPCPath")
	}
	if len(cfg.Items) == 0 {
		panic("infinitescroll: Render requires at least one initial Item — empty feeds should render an empty-state block instead")
	}
	ariaLabel := cfg.AriaLabel
	if ariaLabel == "" {
		ariaLabel = "Feed"
	}
	rootMargin := cfg.RootMargin
	if rootMargin == "" {
		rootMargin = "200px"
	}
	loadMore := cfg.LoadMoreLabel
	if loadMore == "" {
		loadMore = "Load more"
	}

	wrapAttrs := map[string]string{
		"class":                         mergeClass("infinitescroll", cfg.Class),
		"role":                          "feed",
		"aria-label":                    ariaLabel,
		"aria-busy":                     "false",
		"data-fui-infinite-scroll":      cfg.RPCPath,
		"data-fui-infinite-cursor":      cfg.Cursor,
		"data-fui-infinite-items":       ".infinitescroll__items",
		"data-fui-infinite-root-margin": rootMargin,
	}
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}

	itemsAttrs := map[string]string{
		"class": mergeClass("infinitescroll__items", cfg.ItemsClass),
	}
	items := render.Tag("div", itemsAttrs, cfg.Items...)

	sentinel := render.Tag("div", map[string]string{
		"class":                      "infinitescroll__sentinel",
		"data-fui-infinite-sentinel": "",
		"aria-hidden":                "true",
	})

	// <noscript> fallback: keyboard-operable form that submits a Load
	// more request even when JS is disabled.
	noJS := render.Raw(`<noscript><form class="infinitescroll__noscript" action="` +
		htmlEscape(cfg.RPCPath) + `" method="get">` +
		`<input type="hidden" name="cursor" value="` + htmlEscape(cfg.Cursor) + `">` +
		`<button type="submit" class="infinitescroll__loadmore">` + htmlEscape(loadMore) + `</button>` +
		`</form></noscript>`)

	return Style.WrapHTML(render.Tag("div", wrapAttrs, items, sentinel, noJS))
}

func mergeClass(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}

// htmlEscape minimally escapes user-supplied strings injected into a
// raw HTML string. Restricted to the characters that can break out of
// an attribute or text context.
func htmlEscape(s string) string {
	// Hot path: many cursors and labels are alphanumeric.
	for _, c := range s {
		if c == '<' || c == '>' || c == '"' || c == '&' || c == '\'' {
			goto slow
		}
	}
	return s
slow:
	out := make([]byte, 0, len(s)+8)
	for _, c := range s {
		switch c {
		case '<':
			out = append(out, "&lt;"...)
		case '>':
			out = append(out, "&gt;"...)
		case '&':
			out = append(out, "&amp;"...)
		case '"':
			out = append(out, "&quot;"...)
		case '\'':
			out = append(out, "&#39;"...)
		default:
			if c < 128 {
				out = append(out, byte(c))
			} else {
				// multi-byte UTF-8 — pass through unchanged.
				p := make([]byte, 4)
				n := encodeRune(p, c)
				out = append(out, p[:n]...)
			}
		}
	}
	return string(out)
}

// encodeRune encodes r into p as UTF-8 and returns the number of bytes.
// Mirrors utf8.EncodeRune without importing the package for a single use.
func encodeRune(p []byte, r rune) int {
	switch {
	case r < 0x80:
		p[0] = byte(r)
		return 1
	case r < 0x800:
		p[0] = byte(0xC0 | r>>6)
		p[1] = byte(0x80 | r&0x3F)
		return 2
	case r < 0x10000:
		p[0] = byte(0xE0 | r>>12)
		p[1] = byte(0x80 | (r>>6)&0x3F)
		p[2] = byte(0x80 | r&0x3F)
		return 3
	default:
		p[0] = byte(0xF0 | r>>18)
		p[1] = byte(0x80 | (r>>12)&0x3F)
		p[2] = byte(0x80 | (r>>6)&0x3F)
		p[3] = byte(0x80 | r&0x3F)
		return 4
	}
}
