package runtime

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The embed shell links no component sheet: every one loads lazily for the
// content the frame fetches. Writing that content before its sheets arrive
// painted every embed unstyled first. The frame now starts the content's
// sheets and waits for them (bounded) before the markup lands, so the
// component is styled the instant it enters the DOM.
func TestEmbedContentPaintsStyled(t *testing.T) {
	embedJS, err := EmbedJS()
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]http.HandlerFunc{
		"/e.js": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, embedJS)
		},
		"/emb.css": func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(500 * time.Millisecond)
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, `#probe{display:block;width:7px}`)
		},
		"/x": raJSON(`{}`),
		"/c": raHTML(`<div data-cui-comp="emb" id="probe">content</div>`),
		"/frame": raHTML(`<!doctype html><html><head>` +
			`<meta name="gofastr-embed" content='{"surface":"s","exchange":"/x","content":"/c"}'>` +
			`<script>window.__gofastr_catalog={"emb":{"stylePath":"/emb.css","version":"1"}};</script></head>` +
			`<body><div id="gofastr-embed-root" data-cui-embed-state="loading"></div>` +
			`<script>new MutationObserver(() => { const p = document.getElementById('probe');` +
			` if (p && window.__probeW === undefined) window.__probeW = p.getBoundingClientRect().width; })` +
			`.observe(document.body, { childList: true, subtree: true });</script>` +
			`<script src="/e.js"></script></body></html>`),
	}
	s := newRASite(t, raFullJS(t), `<!doctype html><html><body><iframe id="fr" src="/frame" style="width:600px"></iframe></body></html>`, routes)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var w float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.Poll(`(() => { const d = document.getElementById('fr').contentDocument; return !!d && !!d.getElementById('gofastr-embed-root') && !!d.defaultView.__gofastr; })()`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('fr').contentWindow.postMessage({ proto: 'gofastr-embed/1', type: 'token', token: 'nonce' }, '*')`, nil),
		chromedp.Poll(`document.getElementById('fr').contentWindow.__probeW !== undefined`, nil, chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Evaluate(`document.getElementById('fr').contentWindow.__probeW`, &w),
	); err != nil {
		t.Fatal(err)
	}
	if w != 7 {
		t.Errorf("the embedded component was %vpx wide when it entered the DOM, want 7: it painted before its sheet", w)
	}
}
