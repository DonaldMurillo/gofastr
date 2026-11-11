package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// parseDoc drops the head's inline sheets before the parse (no
// style-src report against the live policy). A regex over the head
// read a script's text as markup: a literal "<style>" in a head script
// started a match that ran to the next real </style>, deleting the rest
// of the script and the page-meta and csrf-token metas between, and a
// literal "<body" in one ended the head there, so the sheets after it
// stayed. Script, comment and title text is skipped now (the fixture
// carries a "<style>" in each); the body is left as it came.
func TestParseDocSkipsHeadScriptText(t *testing.T) {
	base, _, _, _ := startStaticModeServer(t, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	type parsed struct {
		CSRF       bool   `json:"csrf"`
		PageMeta   bool   `json:"pageMeta"`
		HeadStyles int    `json:"headStyles"`
		BodyStyles int    `json:"bodyStyles"`
		Script     string `json:"script"`
	}
	docs := map[string]string{
		"style literal": `var x = "<style>";`,
		"body literal":  `if (a <body.length) {}`,
		"close literal": `var y = "</style>";`,
	}
	for name, script := range docs {
		t.Run(name, func(t *testing.T) {
			doc := `<!doctype html><html><head><title>T <style> x</title><script>` + script + `</script>` +
				`<meta name="csrf-token" content="new"><meta data-cui-page-meta name="description" content="d">` +
				`<style>.a{color:red}</style><!-- <style> --></head><body><style>.b{}</style><p>x</p></body></html>`
			var got parsed
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Evaluate(`(() => { const d = window.__gofastr._parseDoc(`+"`"+doc+"`"+`); return {
					csrf: !!d.querySelector('meta[name="csrf-token"]'),
					pageMeta: !!d.querySelector('meta[data-cui-page-meta]'),
					headStyles: d.head.querySelectorAll('style').length,
					bodyStyles: d.body.querySelectorAll('style').length,
					script: d.head.querySelector('script') ? d.head.querySelector('script').textContent : '',
				}; })()`, &got),
			); err != nil {
				t.Fatal(err)
			}
			want := parsed{CSRF: true, PageMeta: true, HeadStyles: 0, BodyStyles: 1, Script: script}
			if got != want {
				t.Errorf("parsed head %+v, want %+v", got, want)
			}
		})
	}
}
