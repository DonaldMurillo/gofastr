package runtime

// Widget chrome (the MountSidebar phone drawer) is fetched and mounted
// after load, so its nav links are not in the document for the
// load-time sweep, and no navigation follows to sweep them. The module
// sweeps a root when the widgets module announces it (fui:widget-open).

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestWidgetOpenMarksCurrentLink(t *testing.T) {
	srv := activelinkSweepServer(t, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	// The mounted root stands in for lazily fetched drawer chrome: the
	// server rendered it with no current path, so nothing is marked.
	const mount = `(() => {
		const root = document.createElement('div');
		root.setAttribute('data-cui-widget', 'ui-sidebar-drawer');
		root.innerHTML = '<nav aria-label="Primary">' +
			'<a id="d-orders" href="/orders" data-cui-activelink>Orders</a>' +
			'<a id="d-sub" href="/settings" data-cui-activelink data-cui-match-prefix="/settings">Settings</a>' +
			'</nav>';
		document.body.appendChild(root);
		document.dispatchEvent(new CustomEvent('fui:widget-open',
			{detail: {name: 'ui-sidebar-drawer', root: root, hydrated: false, reinserted: false}}));
		return true;
	})()`
	var orders, settings string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/settings/profile"),
		chromedp.WaitVisible(`#main`, chromedp.ByID),
		chromedp.Poll(activelinkLoaded, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(mount, nil),
		chromedp.Evaluate(ariaCurrent("d-orders"), &orders),
		chromedp.Evaluate(ariaCurrent("d-sub"), &settings),
	); err != nil {
		t.Fatalf("mount drawer: %v", err)
	}
	if settings != "page" {
		t.Errorf("drawer entry for the current section has aria-current=%q, want page", settings)
	}
	if orders != "" {
		t.Errorf("drawer entry for another page has aria-current=%q, want none", orders)
	}
}
