package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPaneURLWritesCloseToBase(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "nested"}[nested], func(t *testing.T) {
			site := startInterceptStackServer(t)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(site.srv.URL+"/list"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
				t.Fatal(err)
			}
			stackStep(t, ctx, 0)
			if !interceptWait(ctx, `!!document.getElementById('rec-a')`) {
				t.Fatal("record pane did not open")
			}
			if nested {
				stackStep(t, ctx, 1)
				if !interceptWait(ctx, `!!document.getElementById('rel-r1')`) {
					t.Fatal("nested pane did not open")
				}
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr._pushURL(location.pathname+'?page=2');window.__gofastr._pushURL(location.pathname+'?page=3');window.__gofastr._pushURL(location.pathname+'?page=4',{replace:true});document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));true`, nil)); err != nil {
				t.Fatal(err)
			}
			want := `location.pathname==='/list' && !document.getElementById('rec-a')`
			if nested {
				want = `location.pathname==='/rec/a' && !location.search && !!document.getElementById('rec-a') && !document.getElementById('rel-r1')`
			}
			if !interceptWait(ctx, want) {
				t.Fatalf("pane URL writes escaped its history span: %+v", readStack(ctx))
			}
		})
	}
}
