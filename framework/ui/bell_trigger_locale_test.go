package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestBellTriggerRequestLocale(t *testing.T) {
	french := frenchCtx(map[i18n.Phrase]string{
		i18nui.HuiNotificationCount: "{{n}} notifications non lues", i18nui.NotificationEmpty: "Aucune notification",
	})
	cfg := NotificationBellConfig{Name: "inbox", Href: "/inbox", Label: "Inbox {{n}} & news", Ctx: french}
	mountedTrigger, body := NotificationBell(cfg)
	if got := NotificationBellTrigger(french, cfg); got != mountedTrigger {
		t.Fatal("constructor and request renderer disagree for the same context")
	}
	for _, request := range []struct {
		ctx          context.Context
		count, empty string
	}{
		{context.Background(), "unread notifications", "No new notifications"},
		{french, "notifications non lues", "Aucune notification"},
		{nil, "unread notifications", "No new notifications"},
	} {
		for _, count := range []int{0, 23} {
			for _, signal := range []string{"", "inbox.unread"} {
				cfg.UnreadCount, cfg.SignalUnread = count, signal
				trigger := string(NotificationBellTrigger(request.ctx, cfg))
				for _, want := range []string{
					fmt.Sprintf(`aria-label="Inbox {{n}} &amp; news: %d %s"`, count, request.count),
					`data-hui-notification-count-fmt="{{n}} ` + request.count + `"`,
					`data-hui-notification-label="Inbox {{n}} &amp; news"`, `data-cui-open="inbox"`,
				} {
					if !strings.Contains(trigger, want) {
						t.Errorf("request trigger lacks %q: %s", want, trigger)
					}
				}
				if got, want := strings.Contains(trigger, `fui-notification-bell__badge"`), count > 0 || signal != ""; got != want {
					t.Errorf("count %d signal %q: badge = %t, want %t", count, signal, got, want)
				}
			}
		}
		if rendered := renderBody(t, body, request.ctx); !strings.Contains(rendered, request.empty) {
			t.Errorf("one mounted body lost its request empty text: %s", rendered)
		}
	}
}

func TestBellTriggerKeepsCallerText(t *testing.T) {
	cfg := NotificationBellConfig{Name: "inbox", Href: "/inbox", Label: "Delivery alerts", EmptyText: "All delivered"}
	ctx := frenchCtx(map[i18n.Phrase]string{i18nui.NotificationEmpty: "Aucune notification"})
	_, body := NotificationBell(cfg)
	if trigger := string(NotificationBellTrigger(ctx, cfg)); !strings.Contains(trigger, `aria-label="Delivery alerts: 0 unread notifications"`) {
		t.Errorf("request trigger discarded the caller's label: %s", trigger)
	}
	if rendered := renderBody(t, body, ctx); !strings.Contains(rendered, ">Delivery alerts<") || !strings.Contains(rendered, ">All delivered<") {
		t.Errorf("request body discarded caller text: %s", rendered)
	}
}
