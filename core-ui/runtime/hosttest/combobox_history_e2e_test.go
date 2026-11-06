package hosttest

import (
	"context"
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func recordComboboxHistory(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`
		window.historyPushes = 0; window.historyReplaces = 0; window.historyPops = 0; window.navEvents = 0;
		window.programmaticPicks = 0;
		const originalNavigate = __gofastr.navigate;
		__gofastr.navigate = function(...args) { window.programmaticPicks++; return originalNavigate.apply(this, args); };
		const originalPush = history.pushState.bind(history);
		const originalReplace = history.replaceState.bind(history);
		history.pushState = function(...args) { window.historyPushes++; return originalPush(...args); };
		history.replaceState = function(...args) { window.historyReplaces++; return originalReplace(...args); };
		addEventListener('popstate', () => window.historyPops++);
		addEventListener('gofastr:navigate', () => window.navEvents++);
		window.pickedInput = document.getElementById('links');
		window.pickedList = document.getElementById('links-listbox');
	`, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestComboboxPickWritesHistoryOnce(t *testing.T) {
	for _, rows := range []string{"ssr", "island", "search"} {
		for _, method := range []string{"click", "keyboard"} {
			t.Run(rows+"/"+method, func(t *testing.T) {
				server := comboboxLinkSite(t, rows, "", "")
				ctx := comboboxLinkPage(t, server, rows, "")
				recordComboboxHistory(t, ctx)
				const option = `#links-listbox a[href="/birch?view=tree#leaves"]`
				var label string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('`+option+`').getAttribute('data-value')`, &label)); err != nil {
					t.Fatal(err)
				}
				if method == "click" {
					if err := chromedp.Run(ctx, chromedp.Click(option+` span`)); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := chromedp.Run(ctx, chromedp.Click(`#links`)); err != nil {
						t.Fatal(err)
					}
					var active bool
					for attempts := 0; attempts < 6; attempts++ {
						if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('links').getAttribute('aria-activedescendant') === document.querySelector('`+option+`').id`, &active)); err != nil {
							t.Fatal(err)
						}
						if active {
							break
						}
						if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.ArrowDown)); err != nil {
							t.Fatal(err)
						}
					}
					if !active {
						t.Fatal("keyboard did not reach the destination option")
					}
					if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
						if err := input.DispatchKeyEvent(input.KeyRawDown).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13).Do(ctx); err != nil {
							return err
						}
						return input.DispatchKeyEvent(input.KeyUp).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13).Do(ctx)
					})); err != nil {
						t.Fatal(err)
					}
				}
				var pushes int
				var programmatic int
				var picked bool
				if err := chromedp.Run(ctx,
					chromedp.Poll(`window.navEvents > 0 && location.pathname + location.search + location.hash === '/birch?view=tree#leaves' && !!document.getElementById('leaves')`, nil),
					chromedp.Evaluate(`window.historyPushes`, &pushes),
					chromedp.Evaluate(`window.programmaticPicks`, &programmatic),
					chromedp.Evaluate(fmt.Sprintf(`window.pickedInput.value === %q && window.pickedList.hidden && window.pickedInput.getAttribute('aria-expanded') === 'false'`, label), &picked),
				); err != nil {
					t.Fatal(err)
				}
				if pushes != 1 {
					t.Errorf("one pick wrote %d history entries, want 1", pushes)
				}
				wantProgrammatic := 0
				if method == "keyboard" {
					wantProgrammatic = 1
				}
				if programmatic != wantProgrammatic {
					t.Errorf("pick invoked the programmatic navigator %d times, want %d", programmatic, wantProgrammatic)
				}
				if !picked {
					t.Error("pick did not fill the input and close the list")
				}
				var destination string
				if err := chromedp.Run(ctx,
					chromedp.NavigateBack(),
					chromedp.Poll(`window.historyPops === 1`, nil),
					chromedp.Evaluate(`location.pathname + location.search + location.hash`, &destination),
				); err != nil {
					t.Fatal(err)
				}
				if destination != "/" {
					t.Errorf("one Back landed on %q, want /", destination)
				}
			})
		}
	}
}

func TestClaimedComboboxLinkStaysClaimed(t *testing.T) {
	server := comboboxLinkSite(t, "ssr", "", "")
	ctx := comboboxLinkPage(t, server, "ssr", "")
	recordComboboxHistory(t, ctx)
	var preserved bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.beforeNavigations = 0; document.addEventListener('gofastr:beforenavigate', event => { window.beforeNavigations++; event.preventDefault(); })`, nil),
		chromedp.Click(`#links-listbox a span`),
		chromedp.Evaluate(`historyPushes === 0 && programmaticPicks === 0 && beforeNavigations === 1 && location.pathname === '/' && pickedInput.value === 'Birch' && pickedList.hidden`, &preserved),
	); err != nil {
		t.Fatal(err)
	}
	if !preserved {
		t.Fatal("combobox retried a navigation claimed by beforenavigate")
	}
}

func TestNavigateComparesFullDestination(t *testing.T) {
	server := comboboxLinkSite(t, "ssr", "", "")
	ctx := comboboxLinkPage(t, server, "ssr", "")
	recordComboboxHistory(t, ctx)
	if err := chromedp.Run(ctx, chromedp.Click(`#plain-link`), chromedp.Poll(`window.navEvents === 1 && !!document.getElementById('leaves')`, nil)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		path     string
		force    bool
		pushes   int
		replaces int
		events   int
	}{
		{path: "/birch?view=tree#leaves", pushes: 1, events: 1},
		{path: server.URL + "/birch?view=tree#leaves", pushes: 1, events: 1},
		{path: "/birch?view=tree#other", pushes: 2, events: 2},
		{path: "/birch?view=tree", pushes: 3, events: 3},
		{path: "/birch?view=tree", pushes: 3, events: 3},
		{path: "/birch?view=branch#leaves", pushes: 4, events: 4},
		{path: "/birch?view=branch#leaves", force: true, pushes: 4, replaces: 1, events: 5},
		{path: server.URL + "/birch?view=branch#leaves", force: true, pushes: 4, replaces: 2, events: 6},
	} {
		var pushes int
		var replaces int
		var destination string
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`__gofastr.navigate(%q, {force: %t})`, step.path, step.force), nil),
			chromedp.Poll(fmt.Sprintf(`window.navEvents >= %d && !!document.getElementById('leaves')`, step.events), nil),
			chromedp.Evaluate(`location.pathname + location.search + location.hash`, &destination),
			chromedp.Evaluate(`window.historyPushes`, &pushes),
			chromedp.Evaluate(`window.historyReplaces`, &replaces),
		); err != nil {
			t.Fatal(err)
		}
		if pushes != step.pushes {
			t.Errorf("navigate(%q) total pushes=%d, want %d", step.path, pushes, step.pushes)
		}
		if replaces != step.replaces {
			t.Errorf("navigate(%q, force=%t) total replaces=%d, want %d", step.path, step.force, replaces, step.replaces)
		}
		if destination != step.path && server.URL+destination != step.path {
			t.Errorf("navigate(%q) left destination %q", step.path, destination)
		}
	}
}
