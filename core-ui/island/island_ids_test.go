package island

import (
	"context"
	"strings"
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

// derivedThenExplicit derives "features", then names "cart--features"
// explicitly, the way an auto section above Section{ID: ...} does.
type derivedThenExplicit struct{}

func (derivedThenExplicit) Render() render.HTML { return "" }

func (derivedThenExplicit) RenderCtx(ctx context.Context) render.HTML {
	d := render.UniqueID(ctx, "features", "-title")
	render.ReserveID(ctx, "cart--features", "-title")
	return render.HTML(d + " cart--features")
}

func TestIslandExplicitIDWins(t *testing.T) {
	is := NewIsland("cart", derivedThenExplicit{})
	want := render.HTML(`<div data-island="cart">cart--features-2 cart--features</div>`)
	ssr := is.RenderCtx(render.WithIDScope(context.Background()))
	rpc := is.RenderCtx(context.Background())
	if ssr != want || rpc != want {
		t.Errorf("ssr = %s\nrpc = %s\nwant %s", ssr, rpc, want)
	}
}

func TestIslandIDIsEscapedInIDs(t *testing.T) {
	id := `x" onmouseover="alert(1)" data-x="`
	page := render.WithIDScope(context.Background())
	ids := render.NewRegionBatch(page, 1)
	h := NewIsland(id, twoSections{}).RenderCtx(ids.Context(0))
	got := string(ids.Settle(0)(h))
	if strings.Contains(got, `onmouseover="`) {
		t.Errorf("island id reached an attribute raw: %s", got)
	}
}
