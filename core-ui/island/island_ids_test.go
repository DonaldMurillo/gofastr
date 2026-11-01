package island

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// twoSections derives "features" twice, the way two same-titled
// sections do.
type twoSections struct{}

func (twoSections) Render() render.HTML { return "" }

func (twoSections) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML(render.UniqueID(ctx, "features", "-title") + " " + render.UniqueID(ctx, "features", "-title"))
}

func TestIslandIDsMatchOnRerender(t *testing.T) {
	page := render.WithIDScope(context.Background())
	if got := render.UniqueID(page, "features"); got != "features" {
		t.Fatalf("page section: %q", got)
	}
	is := NewIsland("cart", twoSections{})
	ssr := is.RenderCtx(page)
	// The RPC handler's request carries no page scope.
	rpc := is.RenderCtx(context.Background())
	want := render.HTML(`<div data-island="cart">cart--features cart--features-2</div>`)
	if ssr != want || rpc != want {
		t.Errorf("ssr = %s\nrpc = %s\nwant %s", ssr, rpc, want)
	}
	// The page knows the island's ids.
	if got := render.UniqueID(page, "cart--features"); got != "cart--features-3" {
		t.Errorf("page handed out the island's id again: %q", got)
	}
}
