package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestMenuSpaceActivatesLink(t *testing.T) {
	menu := Menu(MenuProps{ID: "links", Label: "Account", Items: []MenuItem{
		{Label: "Profile", Href: "/profile"}, {Label: "Sign out", Href: "/out"},
	}}, nil)
	server := startBehaviorServer(t, string(menu))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, menuLoaded) || !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("menu modules did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__linkClicks = 0;
		document.querySelectorAll('[data-hui-menu="links"] a[role="menuitem"]').forEach(link => {
			link.addEventListener('click', event => { event.preventDefault(); window.__linkClicks++; });
		});
		document.querySelector('[data-hui-menu="links"] > summary').click();
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement.matches('[data-hui-menu="links"] a[role="menuitem"]')`) {
		t.Fatal("menu did not focus its first link")
	}
	var prevented bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const event = new KeyboardEvent('keydown', {key: ' ', bubbles: true, cancelable: true});
		document.activeElement.dispatchEvent(event);
		return event.defaultPrevented;
	})()`, &prevented)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `String(window.__linkClicks)`); got != "1" || !prevented {
		t.Fatalf("Space: clicks = %s, scrolling prevented = %v, want 1 and true", got, prevented)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-menu="links"]').open = true; document.querySelector('[data-hui-menu="links"] a').focus();`, nil)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"s", "i", "g", "n", " ", "o"} {
		if err := chromedp.Run(ctx, menuKey(key)); err != nil {
			t.Fatal(err)
		}
	}
	if got := evalString(t, ctx, `String(window.__linkClicks)`); got != "1" {
		t.Fatalf("Space inside type-ahead activated a link: clicks = %s", got)
	}
	var label string
	if err := chromedp.Run(ctx, menuFocusLabel(&label)); err != nil || label != "Sign out" {
		t.Fatalf("multiword type-ahead focus = %q, error = %v", label, err)
	}
}
