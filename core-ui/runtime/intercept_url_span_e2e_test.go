package runtime

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPaneURLWritesKeepQueryInPane(t *testing.T) {
	for _, write := range []struct {
		name string
		url  string
	}{
		{name: "query", url: `'?p=2'`},
		{name: "hash", url: `'#details'`},
		{name: "absolute", url: `location.origin+'/rec/a?p=2#details'`},
	} {
		for _, replace := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replace=%t", write.name, replace), func(t *testing.T) {
				site := startInterceptStackServer(t)
				ctx := chromedptest.Context(t)
				if err := chromedp.Run(ctx, chromedp.Navigate(site.srv.URL+"/list"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
					t.Fatal(err)
				}
				stackStep(t, ctx, 0)
				if !interceptWait(ctx, `!!document.getElementById('a-sort')`) {
					t.Fatal("record pane did not open")
				}
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(fmt.Sprintf(`window.__gofastr._pushURL(%s,{replace:%t})`, write.url, replace), nil),
					chromedp.Click("#a-sort", chromedp.ByID),
					chromedp.Poll(`location.search==='?sort=name'`, nil),
				); err != nil {
					t.Fatal(err)
				}
				snapshot := readStack(ctx)
				if len(snapshot.Layers) != 1 || snapshot.Main != "list-main" || !idListHas(snapshot.Layers[0].IDs, "rec-a-sorted") {
					t.Fatalf("resolved pane URL lost its query navigation: %+v", snapshot)
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`, nil)); err != nil {
					t.Fatal(err)
				}
				if !interceptWait(ctx, `location.pathname==='/list' && !document.getElementById('cui-intercept')`) {
					t.Fatalf("closing the pane did not consume its URL writes: %+v", readStack(ctx))
				}
			})
		}
	}
}

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

func TestPaneURLWriteOutsideRouteNavigatesPage(t *testing.T) {
	site := startInterceptStackServer(t)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(site.srv.URL+"/list"), chromedp.WaitVisible("#ready", chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	stackStep(t, ctx, 0)
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible("#a-sort", chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr._pushURL(location.origin+'/plain?p=2')`, nil),
		chromedp.Click("#a-sort", chromedp.ByID),
		chromedp.Poll(`document.getElementById('plain-screen')?.textContent==='plain /rec/a?sort=name' && !document.getElementById('cui-intercept')`, nil),
	); err != nil {
		t.Fatal(err)
	}
}
