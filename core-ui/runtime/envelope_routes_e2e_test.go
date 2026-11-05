package runtime

import (
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestEnvelopeRouteMatchesNav(t *testing.T) {
	base := widgetHelpersServer(t, `<script type="application/json" id="gofastr-routes">[
{"path":"/orders/:id:int"},{"path":"/orders/:slug"},
{"path":"/letters/:id:alpha"},{"path":"/letters/:slug"},
{"path":"/keys/:id:uuid"},{"path":"/keys/:slug"},
{"path":"/codes/:id:alnum"},{"path":"/codes/:slug"},
{"path":"/café"},{"path":"/files/:name"},{"path":"/tree/:rest*"}]</script>`)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base),
		chromedp.Poll(`!!window.__gofastr?.loadModule`, nil),
		chromedp.Evaluate(`__gofastr.loadModule('envelope')`, nil),
		chromedp.Poll(`!!__gofastr._navHooks.envelope`, nil),
	); err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct{ path, pattern, param string }{
		{"/orders/help", "/orders/:slug", "help"},
		{"/orders/%31", "/orders/:id:int", "1"},
		{"/letters/42", "/letters/:slug", "42"},
		{"/keys/no", "/keys/:slug", "no"},
		{"/codes/a-b", "/codes/:slug", "a-b"},
		{"/caf%C3%A9", "/café", ""},
		{"/files/%2F", "/files/:name", "/"},
		{"/files/%25", "/files/:name", "%"},
		{"/files/%252F", "/files/:name", "%2F"},
		{"/files/%E0%A4", "/files/:name", "%E0%A4"},
		{"/tree/a%2Fb/%25", "/tree/:rest*", "a/b/%"},
	} {
		t.Run(example.path, func(t *testing.T) {
			var result struct {
				Pattern string `json:"pattern"`
				Param   string `json:"param"`
			}
			expression := `(()=>{const found=__gofastr._navHooks.envelope.manifest(` + strconv.Quote(example.path) + `);return {pattern:found?.entry.path,param:Object.values(found?.params||{})[0]||''}})()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &result)); err != nil {
				t.Fatal(err)
			}
			if result.Pattern != example.pattern || result.Param != example.param {
				t.Fatalf("lookup = %+v, want %s / %s", result, example.pattern, example.param)
			}
		})
	}
}
