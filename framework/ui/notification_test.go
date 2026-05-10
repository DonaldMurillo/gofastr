package ui

import (
	"strings"
	"testing"
)

func TestNotificationRequiresTitle(t *testing.T) {
	defer func() { recover() }()
	Notification(NotificationConfig{})
	t.Fatal("expected panic without Title")
}

func TestNotificationDefaultsToInfo(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "Hello"}))
	if !strings.Contains(h, "ui-notification--info") {
		t.Errorf("expected default info variant, got: %s", h)
	}
	if !strings.Contains(h, `role="status"`) {
		t.Errorf("expected role=status for info, got: %s", h)
	}
}

func TestNotificationDangerGetsAlertRole(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "x", Variant: StatusDanger}))
	if !strings.Contains(h, `role="alert"`) {
		t.Errorf("expected role=alert for danger, got: %s", h)
	}
	if !strings.Contains(h, "ui-notification--danger") {
		t.Errorf("expected danger variant, got: %s", h)
	}
}

func TestNotificationVariantsRenderClass(t *testing.T) {
	for _, v := range []StatusVariant{StatusSuccess, StatusWarning, StatusInfo, StatusNeutral} {
		h := string(Notification(NotificationConfig{Title: "x", Variant: v}))
		want := "ui-notification--" + string(v)
		if !strings.Contains(h, want) {
			t.Errorf("expected %s, got: %s", want, h)
		}
	}
}

func TestNotificationDismissLink(t *testing.T) {
	h := string(Notification(NotificationConfig{
		Title: "Saved", Variant: StatusSuccess, DismissHref: "/notif/dismiss/123",
	}))
	for _, want := range []string{
		`href="/notif/dismiss/123"`,
		`aria-label="Dismiss notification"`,
		"ui-notification__dismiss",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestNotificationOmitsDismissWhenNoHref(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "x"}))
	if strings.Contains(h, "ui-notification__dismiss") {
		t.Errorf("expected no dismiss link, got: %s", h)
	}
}

func TestNotificationBodyRenders(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "Saved", Body: "All changes persisted."}))
	if !strings.Contains(h, "All changes persisted.") {
		t.Errorf("expected body text, got: %s", h)
	}
}

func TestNotificationGlyphPerVariant(t *testing.T) {
	cases := map[StatusVariant]string{
		StatusSuccess: "✓",
		StatusWarning: "!",
		StatusDanger:  "✕",
		StatusInfo:    "i",
		StatusNeutral: "•",
	}
	for v, want := range cases {
		got := notificationGlyph(v)
		if got != want {
			t.Errorf("glyph(%s) = %q, want %q", v, got, want)
		}
	}
}
