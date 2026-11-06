package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPageHeadRejectsUnsafeMetadata(t *testing.T) {
	base := widgetHelpersServer(t, `<meta name="csrf-token" content="old">`)
	ctx := chromedptest.Context(t)
	var safe bool
	if err := chromedp.Run(ctx, chromedp.Navigate(base), chromedp.Poll(`!!window.__gofastr?._applyDocShell`, nil),
		chromedp.Evaluate(`(()=>{
const carrier=document.createElement('template');
carrier.setAttribute('data-cui-page-head', '<meta data-cui-page-meta name="description" content="new" onclick="bad()"><meta data-cui-page-meta property="og:title" content="safe"><meta data-cui-page-meta content="unnamed"><meta data-cui-page-meta name="refresh" http-equiv="refresh" content="0;url=/bad"><link data-cui-page-meta rel="alternate" href="javascript:bad()"><link data-cui-page-meta rel="invalid" href="http://["><link data-cui-page-meta rel="canonical" href="/safe" onload="bad()"><meta name="csrf-token" content="new" onclick="bad()">');
window.__gofastr._applyDocShell(carrier);
return document.head.querySelector('meta[name="description"]')?.content==='new' && document.head.querySelector('meta[property="og:title"]')?.content==='safe' && !document.head.querySelector('meta[content="unnamed"],meta[http-equiv],link[rel="alternate"],link[rel="invalid"],[data-cui-page-meta][onclick],[data-cui-page-meta][onload],meta[name="csrf-token"][onclick]') && document.head.querySelector('link[rel="canonical"]')?.getAttribute('href')==='/safe' && document.head.querySelector('meta[name="csrf-token"]')?.content==='new' && document.head.querySelectorAll('meta[name="csrf-token"]').length===1;
})()`, &safe)); err != nil {
		t.Fatal(err)
	}
	if !safe {
		t.Fatal("navigation copied unsafe head metadata or lost safe metadata")
	}
}
