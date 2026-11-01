package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

type seedCard struct{ HTML string }

// A seed built during an id region carries the settled ids, whatever
// its type.
func TestSeedCarriesSettledIDs(t *testing.T) {
	st := New("seedids")
	text := st.String("text", "")
	html := JSON(st, "html", render.HTML(""))
	card := JSON(st, "card", seedCard{})
	count := st.Int("count", 0)

	ctx := render.WithIDScope(WithValues(context.Background()))
	ids := render.NewRegionBatch(ctx, 1)
	rctx := ids.Context(0)
	render.UniqueID(ctx, "features") // the page took it first
	id := render.UniqueID(rctx, "features")
	text.Seed(rctx, `<h2 id="`+id+`">`)
	html.Seed(rctx, render.HTML(`<h2 id="`+id+`">`))
	card.Seed(rctx, seedCard{HTML: `<h2 id="` + id + `">`})
	count.Seed(rctx, 3)
	ids.Settle(0)("")

	got := ResolveSeed(ctx, []string{text.Name(), html.Name(), card.Name(), count.Name()})
	if v := got[text.Name()]; v != `<h2 id="features-2">` {
		t.Errorf("string seed = %#v", v)
	}
	if v := got[html.Name()]; v != render.HTML(`<h2 id="features-2">`) {
		t.Errorf("render.HTML seed = %#v", v)
	}
	if j, _ := json.Marshal(got[card.Name()]); string(j) != `{"HTML":"\u003ch2 id=\"features-2\"\u003e"}` {
		t.Errorf("struct seed = %q", j)
	}
	if v := got[count.Name()]; v != 3 {
		t.Errorf("int seed = %#v", v)
	}
}
