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

// ── pure-unit: the seed block + escaping ──────────────────────────────

func TestSignalsJSONScript_EmptyIsNoBlock(t *testing.T) {
	if got := signalsJSONScript(nil); got != "" {
		t.Errorf("empty seed should emit no block, got %q", got)
	}
	if got := signalsJSONScript(map[string]any{}); got != "" {
		t.Errorf("empty map should emit no block, got %q", got)
	}
}

func TestSignalsJSONScript_NeutralizesScriptBreakout(t *testing.T) {
	block := signalsJSONScript(map[string]any{"x": `</script><script>alert(1)</script>`})
	if strings.Contains(block, "</script><script>") {
		t.Fatalf("seed block allows <script> breakout: %s", block)
	}
	// Go's json.Marshal escapes '<' to <; belt-and-suspenders
	// escapeJSONForScript handles any residual `</`. Either way the raw
	// closing-tag sequence must not appear inside the data island.
	inner := block[strings.Index(block, ">")+1 : strings.LastIndex(block, "<")]
	if strings.Contains(inner, "</") {
		t.Fatalf("raw </ survived inside the data island: %s", inner)
	}
}

// ── e2e via httptest: producer seeds, consumer + block carry the value ─

var testCompany = store.New("uihosttest").String("company", "DefaultCo").Global()

// seedConsumer is a producer+consumer in one screen: Load seeds the
// per-request value; RenderCtx binds it (a presentational consumer).
type seedConsumer struct{}

func (c *seedConsumer) Load(ctx context.Context) error {
	testCompany.Seed(ctx, "TenantCo")
	return nil
}

func (c *seedConsumer) RenderCtx(ctx context.Context) render.HTML {
	return testCompany.Bind(ctx, "span", map[string]string{"id": "co"})
}

func (c *seedConsumer) Render() render.HTML { return c.RenderCtx(context.Background()) }

func newSeedHost() *UIHost {
	a := app.NewApp("seed-test")
	a.RegisterScreen(app.NewScreen("/", &seedConsumer{}).WithTitle("Seed"), nil)
	return New(a)
}

func TestSeed_ResolvedValueInBlockAndConsumer(t *testing.T) {
	ds := newSeedHost()
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if !strings.Contains(page, `id="gofastr-signals"`) {
		t.Fatalf("missing seed block:\n%s", truncate(page, 800))
	}
	// The seed block carries the per-request value (not the default).
	blockStart := strings.Index(page, `id="gofastr-signals"`)
	block := page[blockStart:]
	block = block[:strings.Index(block, "</script>")]
	if !strings.Contains(block, `"uihosttest.company":"TenantCo"`) {
		t.Errorf("seed block missing resolved value TenantCo: %s", block)
	}
	if strings.Contains(block, "DefaultCo") {
		t.Errorf("seed block should carry request value, not default: %s", block)
	}
	// The consumer DOM stamps the SAME value (no SSR/seed drift).
	if !strings.Contains(page, `data-fui-signal="uihosttest.company"`) || !strings.Contains(page, `>TenantCo</span>`) {
		t.Errorf("consumer did not stamp resolved value:\n%s", truncate(page, 800))
	}
}

func TestSeed_GlobalSeedsEvenWhenNotReferenced(t *testing.T) {
	// A global slice no page element binds must still seed (it's app-wide
	// state read by JS / future consumers).
	_ = store.New("uihosttest").String("flagOnly", "ON").Global()

	a := app.NewApp("seed-test2")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Plain"), nil)
	ds := New(a)
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if !strings.Contains(page, `"uihosttest.flagOnly":"ON"`) {
		t.Errorf("global slice not seeded on a page that doesn't reference it")
	}
}
