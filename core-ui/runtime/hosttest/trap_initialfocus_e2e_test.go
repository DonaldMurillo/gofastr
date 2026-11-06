package hosttest

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestInitialFocusSkipsHidden opens each focus-taking surface with hidden
// controls ahead of its first real stop: focus lands on the first stop,
// with Element.checkVisibility and without it.
func TestInitialFocusSkipsHidden(t *testing.T) {
	hidden := render.HTML(`<div style="visibility:hidden"><button>Hidden</button></div>` +
		`<div style="content-visibility:hidden"><button>Skipped</button></div>` +
		`<details><summary tabindex="-1">Closed</summary><button>Closed child</button></details>` +
		`<div style="visibility:hidden"><input autofocus aria-label="Hidden autofocus"></div>`)
	for _, path := range []string{"native", "fallback"} {
		var beforeBoot []string
		if path == "fallback" {
			beforeBoot = []string{`delete Element.prototype.checkVisibility;`}
		}
		for _, surface := range []string{"widget", "intercept", "panehost"} {
			t.Run(path+"/"+surface, func(t *testing.T) {
				server := comboboxLinkSite(t, "ssr", surface, hidden)
				ctx := comboboxLinkPage(t, server, "ssr", surface, beforeBoot...)
				var initial string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.trapInitialFocus`, &initial)); err != nil {
					t.Fatal(err)
				}
				if initial != "links" {
					t.Errorf("initial focus=%q, want the first visible stop links", initial)
				}
			})
		}
	}
}
