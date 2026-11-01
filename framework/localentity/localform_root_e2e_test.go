package localentity_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A local form's <form> that arrives as the inserted node itself (a
// swap of the wrapper's content) is marked like one that arrives
// inside the wrapper: the kernel hands the scanner that node.
func TestLocalFormMarksInsertedForm(t *testing.T) {
	base := startHost(t)
	tab := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var enctype string
	if err := chromedp.Run(tab,
		chromedp.Navigate(base+"/form"),
		poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['localentity-form'])`),
		chromedp.Evaluate(`(function(){
			var wrap = document.getElementById('`+noteForm+`');
			var f = wrap.querySelector('form');
			f.remove();
			f.removeAttribute('enctype');
			wrap.appendChild(f);
		})()`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('#`+noteForm+` form').getAttribute('enctype') || ''`, &enctype),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if enctype != "application/json" {
		t.Fatalf("inserted local form enctype = %q, want application/json", enctype)
	}
}
