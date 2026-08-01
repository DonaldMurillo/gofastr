package widget

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// chromeCtxKey is a test-only context key for a slot that reads the request.
type chromeCtxKey struct{}

// ctxAwareChromeSlot implements ContextComponent so RenderComponentCtx routes to
// RenderCtx — the path a role-aware nav drawer or tenant-scoped chrome takes.
type ctxAwareChromeSlot struct {
	component.ContextOnly
}

func (ctxAwareChromeSlot) RenderCtx(ctx context.Context) render.HTML {
	if v, ok := ctx.Value(chromeCtxKey{}).(string); ok {
		return render.HTML("ctxuser:" + v)
	}
	return render.HTML("ctxuser:anonymous")
}

// TestRenderChromeCtxThreadsRequestContext: the SSR-inlined chrome path must
// thread the request context into each slot, so a context-aware slot sees the
// signed-in user on FIRST paint (not only when the chrome is lazy-fetched from
// /chrome, which already threaded r.Context). Without this the same slot renders
// anonymous on first paint and personalized once reopened.
func TestRenderChromeCtxThreadsRequestContext(t *testing.T) {
	def := New("ctx-chrome-test").
		Slot("body", ctxAwareChromeSlot{}).
		Build()

	ctx := context.WithValue(context.Background(), chromeCtxKey{}, "alice")
	chrome := RenderChromeCtx(ctx, &def)
	if !strings.Contains(chrome, "ctxuser:alice") {
		t.Fatalf("SSR-inlined chrome must render the slot with the request context; got:\n%s", chrome)
	}

	// The build-time / no-ctx entry point stays anonymous — the static exporter
	// dumps chrome outside any request, so it must not regress.
	bg := RenderChrome(&def)
	if !strings.Contains(bg, "ctxuser:anonymous") {
		t.Fatalf("RenderChrome (no ctx) must fall back to anonymous; got:\n%s", bg)
	}
}
