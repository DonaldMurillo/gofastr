package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The reveal module loads after first paint (it is demand-loaded off its
// marker once the document has parsed). An element already on screen by
// then has been painted visible; hiding it to fade it back in is a blink,
// not an entrance. Only elements still below the fold take the hidden
// state, and they reveal as they scroll in. Content inserted after load
// (a navigation, an island swap) is hidden before it paints, so it keeps
// the entrance.
func TestRevealDoesNotHidePaintedContent(t *testing.T) {
	page := `<!doctype html><html><head><script>
window.__hid = [];
// A record whose OLD class list held cui-hidden proves the element was
// hidden at some point, whatever its classes are by the time we look.
new MutationObserver((ms) => { for (const m of ms) if ((m.oldValue || '').split(' ').includes('cui-hidden')) window.__hid.push(m.target.id); })
  .observe(document.documentElement, { attributes: true, attributeFilter: ['class'], attributeOldValue: true, subtree: true });
</script></head><body style="margin:0">
<h1 id="hero" data-cui-reveal="fade-up">Hero</h1>
<div style="height:3000px"></div>
<p id="below" data-cui-reveal="fade-in">Below</p>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	url := startPollServer(t, page, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var hid []string
	var heroClass, belowClass, insertedClass string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.reveal)`, nil),
		chromedp.Evaluate(`window.__hid`, &hid),
		chromedp.Evaluate(`document.getElementById('hero').className`, &heroClass),
		chromedp.Evaluate(`document.getElementById('below').className`, &belowClass),
	); err != nil {
		t.Fatal(err)
	}
	for _, id := range hid {
		if id == "hero" {
			t.Errorf("the on-screen hero was hidden after it painted (classes now %q): a blink on every load", heroClass)
		}
	}
	if belowClass != "cui-hidden" {
		t.Errorf("below-the-fold element classes = %q, want cui-hidden until it scrolls in", belowClass)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('below').scrollIntoView(); 0`, nil),
		chromedp.Poll(`document.getElementById('below').classList.contains('cui-revealed')`, nil),
		// Inserted content is scanned before it paints, so it keeps the
		// entrance even when it lands in the viewport.
		chromedp.Evaluate(`window.__hid = []; const n = document.createElement('p'); n.id = 'late'; n.setAttribute('data-cui-reveal', 'fade-in'); n.textContent = 'late'; document.getElementById('below').before(n); 0`, nil),
		chromedp.Poll(`window.__hid.includes('late')`, nil),
		chromedp.Poll(`document.getElementById('late').classList.contains('cui-revealed')`, nil),
		chromedp.Evaluate(`document.getElementById('late').className`, &insertedClass),
	); err != nil {
		var dbg string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({hid: window.__hid, below: document.getElementById('below').className, late: (document.getElementById('late')||{}).className, y: scrollY})`, &dbg))
		t.Fatalf("scroll-in / inserted reveal: %v (%s)", err, dbg)
	}
}
