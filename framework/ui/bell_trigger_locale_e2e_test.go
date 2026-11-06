package ui_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestBellRequestLocaleLiveCount(t *testing.T) {
	catalog := i18n.NewMapCatalog()
	catalog.Set("fr", i18nui.HuiNotificationCount.Key(), i18n.Message{Text: "{{n}} notifications non lues"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	french := i18n.WithTranslator(ctx, i18n.NewTranslator(catalog, "en"))
	cfg := ui.NotificationBellConfig{Name: "bell", Href: "/inbox", Label: "Inbox {{n}} & news",
		UnreadCount: 2, SignalUnread: "inbox.unread", ID: "bell", Ctx: french}
	for _, request := range []struct {
		name   string
		ctx    context.Context
		suffix string
	}{
		{"English", context.Background(), "unread notifications"},
		{"French", french, "notifications non lues"},
	} {
		t.Run(request.name, func(t *testing.T) {
			browser := moduleTestCtx(t, string(ui.NotificationBellTrigger(request.ctx, cfg)))
			if !pollJS(browser, moduleLoaded("headless-bell")) {
				t.Fatal("bell behavior did not load")
			}
			if got, want := evalString(browser, `document.getElementById('bell').getAttribute('aria-label')`), "Inbox {{n}} & news: 2 "+request.suffix; got != want {
				t.Fatalf("SSR label = %q, want %q", got, want)
			}
			for _, count := range []int{23, 0, 7} {
				if err := chromedp.Run(browser, chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal('inbox.unread', %d)`, count), nil)); err != nil {
					t.Fatal(err)
				}
				want := fmt.Sprintf("Inbox {{n}} & news: %d %s", count, request.suffix)
				if !pollJS(browser, fmt.Sprintf(`document.getElementById('bell').getAttribute('aria-label') === %q && document.querySelector('#bell [data-hui-notification-count]').textContent === %q`, want, fmt.Sprint(count))) {
					t.Fatalf("signal %d lost the request locale or caller label: %s", count, evalString(browser, `document.getElementById('bell').outerHTML`))
				}
			}
		})
	}
}
