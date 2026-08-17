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
	//
	// This form must stay method="get". A plain-HTML noscript form has
	// no way to carry a CSRF token (the JS path forwards the meta token
	// as a header for exactly this reason), so a POST here is a
	// guaranteed 403 under auth.CSRF's unsafe-method enforcement. The
	// one-handler contract still holds: the handler reads
	// r.FormValue("cursor"), which covers the GET query param and the
	// JS path's form-encoded POST body alike.
	noJS := render.Raw(`<noscript><form class="infinitescroll__noscript" action="` +
		render.Escape(cfg.RPCPath) + `" method="get">` +
		`<input type="hidden" name="cursor" value="` + render.Escape(cfg.Cursor) + `">` +
		`<button type="submit" class="infinitescroll__loadmore">` + render.Escape(loadMore) + `</button>` +
		`</form></noscript>`)

	return Style.WrapHTML(render.Tag("div", wrapAttrs, items, sentinel, noJS))
}

func mergeClass(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}
