package ui

import (
	"strings"
	"testing"
)

func TestNotificationBellRequiresHref(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NotificationBell without Href should panic — a popover-only bell is a dead link without script")
		}
	}()
	NotificationBell(NotificationBellConfig{Name: "nb", Label: "x"})
}

func TestNotificationBellRefusesHashHref(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NotificationBell with Href '#' should panic — '#' is not a destination")
		}
	}()
	NotificationBell(NotificationBellConfig{Name: "nb", Label: "x", Href: "#"})
}

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

// TestBellRowHrefDropsUnsafeScheme pins the URL scheme allow-list on
// notification rows. Items are data-driven (live feeds), so a
// javascript:/data: Href must degrade to "#", never a live link.
func TestBellRowHrefDropsUnsafeScheme(t *testing.T) {
	for _, payload := range []string{"javascript:alert(1)", "data:text/html,<x>"} {
		h := string(renderBellRow(NotificationItem{Title: "T", Href: payload}))
		low := strings.ToLower(h)
		if strings.Contains(low, "javascript:") || strings.Contains(low, "data:") {
			t.Fatalf("unsafe scheme reached bell row href: %s", h)
		}
		if !strings.Contains(h, `href="#"`) {
			t.Fatalf("unsafe href should degrade to #: %s", h)
		}
	}
	// Happy path round-trips.
	h := string(renderBellRow(NotificationItem{Title: "T", Href: "/inbox/1"}))
	if !strings.Contains(h, `href="/inbox/1"`) {
		t.Fatalf("safe href dropped: %s", h)
	}
}

func TestNotificationBellEmitsButtonWithAnchorTrigger(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "Notifications",
		Href: "/notifications",
	})
	h := string(trigger)
	// The trigger is the primitive's ANCHOR: the href is the
	// no-script destination and data-fui-open the popover with
	// script — the same element is both.
	if !strings.Contains(h, `<a `) || !strings.Contains(h, `href="/notifications"`) {
		t.Errorf("trigger should be an anchor to the notifications page:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-open="bell"`) {
		t.Errorf("trigger should open the paired popover via data-fui-open:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-popover-anchor="bottom"`) {
		t.Errorf("trigger should anchor the popover below the bell:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="0 unread notifications"`) {
		t.Errorf("the anchor's name says the spoken count:\n%s", h)
	}
}

func TestNotificationBellBadgeHiddenAtZero(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x", UnreadCount: 0,
		Href: "/notifications",
	})
	h := string(trigger)
	if strings.Contains(h, `fui-notification-bell__badge"`) {
		t.Errorf("UnreadCount=0 should NOT render a badge:\n%s", h)
	}
}

func TestNotificationBellBadgeRendersCount(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x", UnreadCount: 7,
		Href: "/notifications",
	})
	h := string(trigger)
	if !strings.Contains(h, `fui-notification-bell__badge"`) {
		t.Errorf("UnreadCount>0 should render a badge:\n%s", h)
	}
	if !strings.Contains(h, ">7<") {
		t.Errorf("badge should contain the count value:\n%s", h)
	}
}

func TestNotificationBellBadgeOverflow(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x", UnreadCount: 250,
		Href: "/notifications",
	})
	h := string(trigger)
	if !strings.Contains(h, ">250<") {
		t.Errorf("the primitive shows the raw count; the 99+ shaping is the sheet's (the badge class carries it):\n%s", h)
	}
}

func TestNotificationBellSignalBindings(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x",
		SignalUnread: "unread-count",
		SignalList:   "notification-list",
		Href:         "/notifications",
	})
	h := string(trigger)
	if !strings.Contains(h, `data-fui-signal="unread-count"`) {
		t.Errorf("SignalUnread should bind badge to signal:\n%s", h)
	}
}

func TestNotificationBellReturnsPopoverBuilder(t *testing.T) {
	_, pop := NotificationBell(NotificationBellConfig{
		Name: "bell", Label: "x",
		Href: "/notifications",
	})
	if pop == nil {
		t.Fatal("NotificationBell should return non-nil *widget.Builder")
	}
	def := pop.Definition()
	if def.Name != "bell" {
		t.Errorf("popover name should match bell name, got %q", def.Name)
	}
}

func TestNotificationBellExtraAttrsCannotOverrideOwned(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "nb", Label: "Notifications", UnreadCount: 3, Href: "/notifications",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "type": "evil", "Class": "evil",
		},
	})
	root := string(trigger)[:strings.Index(string(trigger), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("bell button missing data-test:\n%s", root)
	}
	// The anchor's own contract: the href and the spoken count. The
	// old button's type and the describedby paragraph are the
	// primitive's; here the count rides the anchor's name and the
	// badge's hook.
	for _, want := range []string{`href="/notifications"`, `aria-label="3 unread notifications"`} {
		if !strings.Contains(string(trigger), want) {
			t.Errorf("owned attr lost its framework value (%q):\n%s", want, trigger)
		}
	}
	if strings.Contains(root, "evil") {
		t.Errorf("owned attr overridden by ExtraAttrs:\n%s", root)
	}
}

func TestNotificationBellZeroUnreadDropsDescribedbyVariant(t *testing.T) {
	// With no unread count the component never re-asserts
	// aria-describedby after the merge, so this state proves the
	// sanitizer's case-insensitive drop alone keeps the attr out.
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name: "nb", Label: "Notifications", Href: "/notifications",
		ExtraAttrs: map[string]string{"ARIA-DESCRIBEDBY": "evil"},
	})
	root := string(trigger)[:strings.Index(string(trigger), ">")+1]
	if strings.Contains(strings.ToLower(root), "aria-describedby") {
		t.Errorf("zero-unread bell must carry no aria-describedby:\n%s", root)
	}
}
