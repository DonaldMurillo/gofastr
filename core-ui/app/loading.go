package app

// — where loading content is
// declared and how it reaches the browser. A loading declaration is
// per OUTLET (OutletSpec.Loading), per PRIMARY SLOT (LayoutSpec.
// Primary), or per ROUTE AREA (AreaSpec.Loading, 2026-09-26): the
// server renders the component ONCE into an inert
// <template data-fui-loading="<addr>"> beside the outlet, slot, or
// area cell, so the browser already holds the bytes BEFORE any
// navigation fetch starts;
import (
	"context"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Loading declares what an outlet, the primary slot, or a route area
// shows while a navigation that will change it is in flight. The zero
// Loading (or a nil pointer) means the busy dim only.
type Loading struct {
	// Show renders the loading content: a skeleton, a spinner, any
	// component. Shown only in place of the outlet's previous content
	// while the navigation response is still pending; nil Show means
	// no template is emitted (the dim only). No Load, no DI, no route
	// params: loading content is presentational and must not fetch.
	Show component.Component
	// After shows the loading content only when the wait exceeds this
	// duration, so a fast response paints nothing extra (the same
	// anti-flash contract the dim's CSS transition delay gives).
	// Zero means the default, the dim's .12s delay; negative is
	// clamped to it.
	After time.Duration
	// Min keeps the loading content, once shown, at least this long
	// before the response replaces it (no skeleton flash). Zero is no
	// minimum. Negative is clamped to zero.
	Min time.Duration
}

// defaultLoadingAfter matches the dim's CSS transition delay
// (.12s, frameworkBuiltinCSS): a loading declaration without an
// explicit After behaves like the dim it replaces.
const defaultLoadingAfter = 120 * time.Millisecond

// loadingAfterMs resolves After to the wire's whole milliseconds.
func (ld *Loading) loadingAfterMs() int {
	if ld == nil || ld.After <= 0 {
		return int(defaultLoadingAfter / time.Millisecond)
	}
	return int(ld.After / time.Millisecond)
}

// loadingMinMs resolves Min to the wire's whole milliseconds.
func (ld *Loading) loadingMinMs() int {
	if ld == nil || ld.Min <= 0 {
		return 0
	}
	return int(ld.Min / time.Millisecond)
}

// loadingTemplate renders the inert loading template for one address
// (an outlet's "<layer>#<name>" or the primary's bare layer key).
// nil Loading or nil Show emits nothing. The content renders under the
// same panic-to-error containment every component render gets: a
// panicking loading component degrades to no loading content, never a
// broken page. The template is a SIBLING of its outlet cell, not a
// child: fill application replaces the cell's innerHTML, and a child
// template would be destroyed by the first navigation it exists for.
func loadingTemplate(ctx context.Context, addr string, ld *Loading) render.HTML {
	if ld == nil || ld.Show == nil {
		return ""
	}
	content, renderErr := component.SafeRenderCtx(ctx, ld.Show)
	if renderErr != nil {
		content = ""
	}
	return render.Tag("template", map[string]string{
		"data-fui-loading": addr,
		"data-fui-after":   strconv.Itoa(ld.loadingAfterMs()),
		"data-fui-min":     strconv.Itoa(ld.loadingMinMs()),
	}, content)
}

// LoadingComponent adapts plain markup (ui.SkeletonRow(…), ui.Spinner(…))
// to the Loading.Show slot: the presets return render.HTML, not a
// component, and Loading wants a component so ANY piece composes.
func LoadingComponent(h render.HTML) component.Component {
	return loadingMarkup{h}
}

type loadingMarkup struct{ html render.HTML }

func (m loadingMarkup) Render() render.HTML { return m.html }
