package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestBellKeepsLabelWithLiveCount(t *testing.T) {
	words := DefaultStrings()
	words.NotificationCount = "{{n}} pendientes"
	body := NotificationBell(NotificationBellProps{ID: "inbox", Href: "/inbox", Label: "Inbox",
		UnreadBind: &Bind{Signal: "inbox"}}, nil) +
		NotificationBell(NotificationBellProps{ID: "alerts", Href: "/alerts", Label: "Alerts {{n}} & news",
			UnreadCount: 7, UnreadBind: &Bind{Signal: "alerts"}, Strings: words}, nil)
	ctx := behaviorPage(t, startBehaviorServer(t, string(body)))
	if !pollTrue(ctx, controlsLoadedExpr(BellBehaviorName)) {
		t.Fatal("bell behavior did not load")
	}
	var initial bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('inbox').getAttribute('aria-label') === 'Inbox: 0 unread notifications' &&
		document.getElementById('alerts').getAttribute('aria-label') === 'Alerts {{n}} & news: 7 pendientes'`, &initial)); err != nil {
		t.Fatal(err)
	}
	if !initial {
		t.Error("server bell name discarded its required label")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('inbox', 23); window.__gofastr.setSignal('alerts', 0);`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('inbox').getAttribute('aria-label') === 'Inbox: 23 unread notifications' &&
		document.getElementById('alerts').getAttribute('aria-label') === 'Alerts {{n}} & news: 0 pendientes'`) {
		t.Fatal("signal update lost the bell label or substituted the label's literal placeholder")
	}
}
