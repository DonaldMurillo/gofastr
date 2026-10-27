package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// An element edit on the page the visitor is looking at refreshes that
// page, and so does undoing it. The reload client refreshed only on
// add/delete page and route, and undo sent no event, so both left the
// old screen up until a manual reload. The refresh is an SPA navigation:
// a marker set on window before the edit survives it.
func TestBrowser_PageEditRefreshesScreen(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx := chromedptest.Context(t)

	if res := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path: "/dashboard", Title: "Dashboard",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Before edit"}},
		}},
	}}); !res.OK {
		t.Fatalf("add_page: %+v", res)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/dashboard"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitJS(t, ctx, `document.body.classList.contains('cui-sse-up')`, "reload client never connected")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__kilnNoHardReload = 1`, nil)); err != nil {
		t.Fatal(err)
	}

	headingID := tools.Live().Session().World.Pages["/dashboard"].Tree.Children[0].ID
	if res := tools.UpdatePageElement(t.Context(), protocol.UpdatePageElementArgs{
		Path: "/dashboard", ElementID: headingID,
		Patch: protocol.PageElementOp{Op: "set_props", SetProps: map[string]any{"text": "After edit"}},
	}); !res.OK {
		t.Fatalf("update_page_element: %+v", res)
	}
	waitH1(t, ctx, "After edit")

	if res := tools.Undo(t.Context(), protocol.UndoArgs{}); !res.OK {
		t.Fatalf("undo: %+v", res)
	}
	waitH1(t, ctx, "Before edit")

	var marker bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__kilnNoHardReload === 1`, &marker)); err != nil {
		t.Fatal(err)
	}
	if !marker {
		t.Error("refresh was a hard reload: the window marker did not survive")
	}
}

func waitH1(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	var got string
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`(document.querySelector('h1') || {}).textContent || ''`, &got)); err == nil &&
			strings.Contains(got, want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("h1 = %q after 6s, want %q: the screen did not refresh", got, want)
}

func waitJS(t *testing.T, ctx context.Context, expr, msg string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err == nil && ok {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatal(msg)
}

// A theme edit and a nav edit change the document shell, not the screen
// under it: the stylesheet in the head and the layout's sidebar. A forced
// SPA navigation keeps both, so the page kept the old colors and nav
// until a manual reload. Undoing the theme edit restores the old colors
// the same way.
func TestBrowser_ShellEditRefreshesDocument(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx := chromedptest.Context(t)

	if res := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path: "/dashboard", Title: "Dashboard",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Dashboard"}},
		}},
	}}); !res.OK {
		t.Fatalf("add_page: %+v", res)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/dashboard"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitJS(t, ctx, `document.body.classList.contains('cui-sse-up')`, "reload client never connected")

	// The light palette carries the edit; the headless browser may render
	// dark, so read the head's styles rather than the computed token.
	const themed = `[...document.querySelectorAll('style')].some(s => s.textContent.includes('--color-primary:#123456'))`
	if res := tools.SetTheme(t.Context(), protocol.SetThemeArgs{Theme: map[string]string{"primary": "#123456"}}); !res.OK {
		t.Fatalf("set_theme: %+v", res)
	}
	waitJS(t, ctx, themed, "theme edit did not reach the page's stylesheet")

	if res := tools.Undo(t.Context(), protocol.UndoArgs{}); !res.OK {
		t.Fatalf("undo: %+v", res)
	}
	waitJS(t, ctx, `document.body.classList.contains('cui-sse-up') && document.querySelector('h1') && !`+themed,
		"undoing the theme edit did not restore the stylesheet")

	if res := tools.SetScaffold(t.Context(), protocol.SetScaffoldArgs{Nav: []world.NavItem{
		{Label: "Quarterly reports", Href: "/dashboard"},
	}}); !res.OK {
		t.Fatalf("set_scaffold: %+v", res)
	}
	waitJS(t, ctx, `[...document.querySelectorAll('nav a')].some(a => a.textContent.includes('Quarterly reports'))`,
		"nav edit did not reach the page's sidebar")
}

// An edit that lands after a page is rendered but before its event
// stream connects reaches no one: the broadcaster has no replay. The
// stream's ready event names the world the server holds now, the page
// names the world it was rendered from, and a mismatch reloads the
// document. The test holds the page's stream request while the edit
// lands, the window a reload after a shell edit opened in CI.
func TestBrowser_EditBeforeConnectReloads(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx := chromedptest.Context(t)

	if res := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path: "/dashboard", Title: "Dashboard",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Dashboard"}},
		}},
	}}); !res.OK {
		t.Fatalf("add_page: %+v", res)
	}

	held := make(chan fetch.RequestID, 8)
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*fetch.EventRequestPaused); ok {
			held <- e.RequestID
		}
	})
	if err := chromedp.Run(ctx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*/.kiln/events*"}}),
		chromedp.Navigate(urlBase+"/dashboard"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var first fetch.RequestID
	select {
	case first = <-held:
	case <-time.After(6 * time.Second):
		t.Fatal("the page never opened its event stream")
	}

	const themed = `[...document.querySelectorAll('style')].some(s => s.textContent.includes('--color-primary:#123456'))`
	if res := tools.SetTheme(t.Context(), protocol.SetThemeArgs{Theme: map[string]string{"primary": "#123456"}}); !res.OK {
		t.Fatalf("set_theme: %+v", res)
	}
	// Release the held stream, and every later one (the reloaded page's).
	go func() {
		id := first
		for {
			if err := chromedp.Run(ctx, fetch.ContinueRequest(id)); err != nil {
				return
			}
			select {
			case id = <-held:
			case <-ctx.Done():
				return
			}
		}
	}()
	waitJS(t, ctx, themed, "an edit made before the stream connected never reached the page")
}
