package runtime

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestSidebarActiveLinkContracts(t *testing.T) {
	cases := []struct{ name, path, markup, check string }{
		{"nested groups", "/docs/start", `<nav><details id="outer"><summary>Outer</summary><details id="inner"><summary>Inner</summary><a href="/docs/start" data-cui-activelink>Start</a></details></details></nav>`, `document.getElementById('outer').open && document.getElementById('inner').open`},
		{"button groups", "/docs/start", `<nav><ul><li><button id="toggle" data-hui-sidebar-group-toggle aria-controls="children" aria-expanded="false">Group</button><ul id="children" hidden><li><a href="/docs/start" data-cui-activelink>Start</a></li></ul></li></ul></nav>`, `!document.getElementById('children').hidden && document.getElementById('toggle').getAttribute('aria-expanded') === 'true'`},
		{"Unicode path", "/gu%C3%ADa", `<nav><a id="current" href="/guía" data-cui-activelink>Guía</a></nav>`, `document.getElementById('current').getAttribute('aria-current') === 'page'`},
		{"encoded separator remains distinct", "/docs/a%2Fb", `<nav><a id="different" href="/docs/a/b" data-cui-activelink>Different route</a></nav>`, `!document.getElementById('different').hasAttribute('aria-current')`},
		{"external link remains distinct", "/docs/start", `<nav><a id="external" href="https://external.invalid/docs/start" data-cui-activelink>External</a></nav>`, `!document.getElementById('external').hasAttribute('aria-current')`},
		{"stale drawer selection", "/blog/archive", `<nav><a id="stale" href="/blog" data-cui-activelink aria-current="page">All posts</a><a id="current" href="/blog/archive" data-cui-activelink>Archive</a></nav>`, `!document.getElementById('stale').hasAttribute('aria-current') && document.getElementById('current').getAttribute('aria-current') === 'page'`},
		{"outer menu stays closed", "/docs/start", `<details id="menu"><summary>Menu</summary><nav><a href="/docs/start" data-cui-activelink>Start</a></nav></details>`, `!document.getElementById('menu').open`},
		{"scroll spy retains ownership", "/", `<nav data-hui-toc><a id="fragment" href="#title" aria-current="true">Title</a></nav>`, `document.getElementById('fragment').getAttribute('aria-current') === 'true'`},
	}
	srv := activelinkSweepServer(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
			markup, _ := json.Marshal(tc.markup)
			var passed bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+tc.path),
				chromedp.Poll(activelinkLoaded, nil),
				chromedp.Evaluate(`(() => {
					const root = document.createElement('div');
					root.innerHTML = `+string(markup)+`;
					document.body.appendChild(root);
					document.dispatchEvent(new CustomEvent('fui:widget-open', {detail: {name: 'sidebar', root}}));
					return `+tc.check+`;
				})()`, &passed),
			); err != nil {
				t.Fatal(err)
			}
			if !passed {
				t.Fatalf("sidebar contract failed: %s", tc.name)
			}
		})
	}
}
