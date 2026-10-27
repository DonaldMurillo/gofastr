package uihost

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var (
	pageNamesQty  = store.New("uihostpn").Int("qty", 3)
	pageNamesCart = store.New("uihostpn").Int("cart", 0).Global()
)

type pageNamesScreen struct{}

func (pageNamesScreen) RenderCtx(ctx context.Context) render.HTML {
	return pageNamesQty.Bind(ctx, "span", nil) + pageNamesCart.Bind(ctx, "span", nil)
}

func (s pageNamesScreen) Render() render.HTML { return s.RenderCtx(context.Background()) }

// A full page names its seed's page-scoped keys beside the flat seed,
// so the runtime can restore exactly those when Back replays the first
// page or a navigation swaps in a fetched document; a global it seeds
// stays off the list (its first-seen rule must hold).
func TestSeedNamesPageScopedKeys(t *testing.T) {
	a := app.NewApp("seed-page-names")
	a.RegisterScreen(app.NewScreen("/", pageNamesScreen{}).WithTitle("Names"), nil)
	srv := httptest.NewServer(New(a))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	const open = `<script type="application/json" id="gofastr-signals-page">`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("no #gofastr-signals-page block:\n%s", truncate(page, 1200))
	}
	block := page[i+len(open):]
	block = block[:strings.Index(block, "</script>")]
	if !strings.Contains(block, `"uihostpn.qty"`) {
		t.Errorf("page-scoped key missing from the list: %s", block)
	}
	if strings.Contains(block, "uihostpn.cart") {
		t.Errorf("a global is listed as page-scoped: %s", block)
	}
	if head := page[:strings.Index(page, "</head>")]; !strings.Contains(head, open) {
		t.Error("the block must sit in <head> beside the seed")
	}
}

func TestSignalPageNamesEmptyIsNoBlock(t *testing.T) {
	if got := signalPageNamesScript(map[string]any{"uihostpn.cart": 0}); got != "" {
		t.Errorf("a seed of globals only must emit no block, got %q", got)
	}
}
