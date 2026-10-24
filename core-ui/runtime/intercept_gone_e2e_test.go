package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A drawer opened onto a record deleted since the reader last visited
// its page: the overlay answers 404, the runtime falls back to an
// ordinary navigation, and that navigation must fetch the 404 page,
// never replay the record page the screen cache still holds.
func TestInterceptMissingRecordSkipsCache(t *testing.T) {
	s, ctx := cachedRecordRig(t)
	s.gone.Store(true)
	stackStep(t, ctx, 0)
	assertGonePage(t, ctx)
}

// The same through a history refetch whose URL carries a #fragment:
// cache keys never do, so the drop must strip it to reach the cached
// page.
func TestInterceptGoneRefetchWithHash(t *testing.T) {
	s, ctx := cachedRecordRig(t)
	stackStep(t, ctx, 0)
	if !interceptWait(ctx, `!!document.getElementById('rec-a')`) {
		t.Fatalf("the drawer never opened: %+v", readStack(ctx))
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.replaceState(history.state, '', '/rec/a#details'); true`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#pg-2`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `!!document.getElementById('rec-a-page-2')`) {
		t.Fatalf("the pane's page 2 never rendered: %+v", readStack(ctx))
	}
	s.gone.Store(true)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	assertGonePage(t, ctx)
}

// cachedRecordRig visits /rec/a as a page, so the screen cache holds
// it, and returns to the list.
func cachedRecordRig(t *testing.T) (*interceptStackServer, context.Context) {
	t.Helper()
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(s.srv.URL+"/list"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('/rec/a'); true`, nil)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `!!document.getElementById('plain-screen') && location.pathname === '/rec/a'`) {
		t.Fatalf("the record page never rendered: %+v", readStack(ctx))
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('/list'); true`, nil)); err != nil {
		t.Fatal(err)
	}
	if !interceptWait(ctx, `location.pathname === '/list' && !!document.getElementById('to-a')`) {
		t.Fatalf("never got back to the list: %+v", readStack(ctx))
	}
	return s, ctx
}

// assertGonePage waits for the fallback and requires the fetched 404
// page at /rec/a, not the cached record and not a drawer.
func assertGonePage(t *testing.T, ctx context.Context) {
	t.Helper()
	if !interceptWait(ctx, `location.pathname === '/rec/a' && (!!document.getElementById('gone') || !!document.getElementById('plain-screen'))`) {
		t.Fatalf("nothing rendered for the missing record: %+v", readStack(ctx))
	}
	time.Sleep(300 * time.Millisecond)
	var gone bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.getElementById('gone') && !document.getElementById('plain-screen')`, &gone)); err != nil {
		t.Fatal(err)
	}
	if st := readStack(ctx); pathOnly(st.URL) != "/rec/a" || len(st.Layers) != 0 || !gone {
		t.Fatalf("after opening the deleted record: %+v (404 page shown: %v); want the 404 page, not the cached record", st, gone)
	}
}

func pathOnly(u string) string { return strings.SplitN(u, "?", 2)[0] }
