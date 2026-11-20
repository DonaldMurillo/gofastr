package app

import (
	"context"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Loading declares what an outlet, the primary slot, or a route area shows while a navigation that
// will change it is in flight.
type Loading struct {
	// Show renders the loading content: a skeleton, a spinner, any component.
	Show component.Component
	// After shows the loading content only when the wait exceeds this duration, so a fast response
	// paints nothing extra (the same anti-flash contract the dim's CSS transition delay gives).
	After time.Duration
	// Min keeps the loading content, once shown, at least this long before the response replaces it (no
	// skeleton flash).
	Min time.Duration
}

const defaultLoadingAfter = 120 * time.Millisecond

func (ld *Loading) loadingAfterMs() int {
	if ld == nil || ld.After <= 0 {
		return int(defaultLoadingAfter / time.Millisecond)
	}
	return int(ld.After / time.Millisecond)
}

func (ld *Loading) loadingMinMs() int {
	if ld == nil || ld.Min <= 0 {
		return 0
	}
	return int(ld.Min / time.Millisecond)
}

func loadingTemplate(ctx context.Context, addr string, ld *Loading) render.HTML {
	if ld == nil || ld.Show == nil {
		return ""
	}
	content, renderErr := component.SafeRenderCtx(ctx, ld.Show)
	if renderErr != nil {
		content = ""
	}
	return render.Tag("template", map[string]string{
		"data-cui-loading": addr,
		"data-cui-after":   strconv.Itoa(ld.loadingAfterMs()),
		"data-cui-min":     strconv.Itoa(ld.loadingMinMs()),
	}, content)
}

// LoadingComponent adapts plain markup to the Loading.Show slot.
func LoadingComponent(h render.HTML) component.Component {
	return loadingMarkup{h}
}

type loadingMarkup struct{ html render.HTML }

func (m loadingMarkup) Render() render.HTML { return m.html }
