package ui

import (
	"strings"
	"testing"
)

func TestNotificationBellRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NotificationBell without Name should panic")
		}
	}()
	NotificationBell(NotificationBellConfig{Label: "x"})
}

func TestNotificationBellRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NotificationBell without Label should panic")
		}
	}()
	NotificationBell(NotificationBellConfig{Name: "x"})
}

func TestNotificationBellEmitsButtonWithAnchorTrigger(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "Notifications",
	})
	h := string(trigger)
	if !strings.Contains(h, "<button ") {
		t.Errorf("trigger should be a <button>:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-open="bell"`) {
		t.Errorf("trigger should open the paired popover via data-fui-open:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-popover-anchor="bottom"`) {
		t.Errorf("trigger should anchor the popover below the bell:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Notifications"`) {
		t.Errorf("trigger should have aria-label=Label:\n%s", h)
	}
}

func TestNotificationBellBadgeHiddenAtZero(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x", UnreadCount: 0,
	})
	h := string(trigger)
	if strings.Contains(h, "ui-notification-bell__badge") {
		t.Errorf("UnreadCount=0 should NOT render a badge:\n%s", h)
	}
}

func TestNotificationBellBadgeRendersCount(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x", UnreadCount: 7,
	})
	h := string(trigger)
	if !strings.Contains(h, "ui-notification-bell__badge") {
		t.Errorf("UnreadCount>0 should render a badge:\n%s", h)
	}
	if !strings.Contains(h, ">7<") {
		t.Errorf("badge should contain the count value:\n%s", h)
	}
}

func TestNotificationBellBadgeOverflow(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x", UnreadCount: 250,
	})
	h := string(trigger)
	if !strings.Contains(h, ">99+<") {
		t.Errorf("count >99 should render as '99+':\n%s", h)
	}
}

func TestNotificationBellSignalBindings(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x",
		SignalUnread: "unread-count",
		SignalList:   "notification-list",
	})
	h := string(trigger)
	if !strings.Contains(h, `data-fui-signal="unread-count"`) {
		t.Errorf("SignalUnread should bind badge to signal:\n%s", h)
	}
}

func TestNotificationBellReturnsPopoverBuilder(t *testing.T) {
	_, pop := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x",
	})
	if pop == nil {
		t.Fatal("NotificationBell should return non-nil *widget.Builder")
	}
	def := pop.Definition()
	if def.Name != "bell" {
		t.Errorf("popover name should match bell name, got %q", def.Name)
	}
}
