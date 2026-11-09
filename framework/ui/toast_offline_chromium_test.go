package ui_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func toastOfflineServer(t *testing.T, opts ...uihost.Option) *httptest.Server {
	t.Helper()
	site := app.NewApp("Offline")
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(render.HTML(`<main><h1>Page</h1></main>`))), nil)
	host := uihost.New(site, opts...)
	fw := framework.NewApp()
	fw.Use(host.RouteMatchMiddleware())
	fw.Mount(host)
	if err := fw.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	host.ValidateBoot()
	srv := httptest.NewServer(fw.Router())
	t.Cleanup(srv.Close)
	return srv
}

// headSheetNames lists the component sheets the document's head
// carries: the inline bundle's names, or the linked bundle's or
// sheets' names.
func headSheetNames(t *testing.T, url string) map[string]bool {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	head := doc[:strings.Index(doc, "</head>")]
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-cui-(?:bundle|style)="([^"]+)"`).FindAllStringSubmatch(head, -1) {
		for _, n := range strings.Split(m[1], ",") {
			got[n] = true
		}
	}
	return got
}

// The toast that says the network is gone cannot fetch its own sheet.
// The stack's row lives in a <template>, so its sheet loaded only when
// the first toast was cloned; offline that fetch failed and the error
// toast drew as bare text. The initial document now carries the
// stack's sheets, inline or linked.
func TestOfflineToastStylesInFirstDocument(t *testing.T) {
	for name, opts := range map[string][]uihost.Option{
		"inline": nil,
		"linked": {uihost.WithNoInlineFirstPaint()},
	} {
		t.Run(name, func(t *testing.T) {
			got := headSheetNames(t, toastOfflineServer(t, opts...).URL+"/")
			for _, want := range []string{"ui-toast-stack", "ui-notification"} {
				if !got[want] {
					t.Errorf("head lacks the %s sheet: %v", want, got)
				}
			}
		})
	}
}

// Offline, a runtime error toast draws as the kit's notification: the
// row's grid layout applies with no fetch.
func TestOfflineToastIsStyled(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	var largeAppCSS strings.Builder
	largeAppCSS.WriteString(":root{")
	for i := range 1800 {
		sum := sha256.Sum256(fmt.Appendf(nil, "%d", i))
		fmt.Fprintf(&largeAppCSS, "--offline-probe-%d:%x;", i, sum[:16])
	}
	largeAppCSS.WriteString("}")
	for _, tc := range []struct {
		name string
		opts []uihost.Option
	}{
		{name: "inline"},
		{name: "linked-app", opts: []uihost.Option{uihost.WithCustomCSS(largeAppCSS.String())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := toastOfflineServer(t, tc.opts...)
			ctx := chromedptest.Context(t)
			var display string
			var subset, linkedApp bool
			if err := chromedp.Run(ctx,
				network.Enable(),
				network.SetBlockedURLs().WithURLPatterns([]*network.BlockPattern{{URLPattern: srv.URL + "/__gofastr/comp-bundle.css*", Block: true}}),
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitReady("main", chromedp.ByQuery),
				chromedp.Poll(`!!window.__gofastr`, nil),
				chromedp.Evaluate(`!!document.querySelector('style[data-cui-bundle]')`, &subset),
				chromedp.Evaluate(`!!document.querySelector('link#cui-app-css')`, &linkedApp),
				chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := network.EmulateNetworkConditionsByRule(true, []*network.Conditions{{URLPattern: ""}}).Do(ctx)
					return err
				}),
				chromedp.Evaluate(`window.__gofastr._toastOrFallback({variant: 'error', title: 'Request failed', ttl: 60000}); true`, nil),
				chromedp.Poll(`!!document.querySelector('[data-cui-toast-stack] [data-cui-comp="ui-notification"]')`, nil),
				chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-cui-toast-stack] [data-cui-comp="ui-notification"]')).display`, &display),
			); err != nil {
				t.Fatal(err)
			}
			if display != "grid" {
				t.Errorf("offline toast row display = %q, want grid (the notification sheet never applied)", display)
			}
			if tc.name == "linked-app" && (!subset || !linkedApp) {
				t.Fatal("fixture did not inline the component subset beside linked app.css")
			}
		})
	}
}
