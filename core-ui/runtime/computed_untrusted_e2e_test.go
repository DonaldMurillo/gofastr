package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A computed signal inherits its dependencies' trust. A value seeded
// from the URL is set {untrusted: true} so an html-mode binding renders
// it as text; passed through a reducer it came out as a plain
// setSignal, and the computed's html-mode binding parsed the URL's
// markup. A trusted write to the dependency makes the result markup
// again.
func TestComputedKeepsUntrustedFromDeps(t *testing.T) {
	url := computedServer(t, ``, `
  <div id="out" data-cui-signal="page.out" data-cui-signal-mode="html" data-cui-computed="echo" data-cui-computed-deps="page.q"></div>`,
		`R.echo = (v) => v['page.q'] == null ? '' : String(v['page.q']);`)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var evil bool
	var text string
	var trusted bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr.loadedModules || {}).computed && !!window.__gofastr._reducers.echo`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.setSignal('page.q', '<img id="evil" src="x">', {untrusted: true}); true`, nil),
		chromedp.Evaluate(`!!document.getElementById('evil')`, &evil),
		chromedp.Evaluate(`document.getElementById('out').textContent`, &text),
		chromedp.Evaluate(`window.__gofastr.setSignal('page.q', '<b id="fine">ok</b>'); !!document.getElementById('fine')`, &trusted),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if evil || text != `<img id="evil" src="x">` {
		t.Fatalf("an untrusted dependency reached the html binding as markup: element=%v text=%q", evil, text)
	}
	if !trusted {
		t.Fatal("a trusted dependency write must render the computed html as markup again")
	}
}
