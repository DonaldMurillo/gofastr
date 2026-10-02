package ui

import (
	"github.com/DonaldMurillo/gofastr/framework/headless"

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
	if !strings.Contains(h, "fui-notification--info") {
		t.Errorf("expected default info variant, got: %s", h)
	}
	// A polite toast carries no role of its own: the stack it lives
	// in is the live region, and the row announcing itself twice is
	// the defect the primitive exists to prevent.
	if strings.Contains(h, `role="alert"`) {
		t.Errorf("an info notification must not interrupt, got: %s", h)
	}
}

// TestNotificationRejectsUnknownVariant mirrors Button/StatusBadge/
// Callout. A typo'd variant must panic instead of silently emitting
// an unmatched class.
func TestNotificationRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for unknown Notification Variant, got none")
		}
	}()
	_ = Notification(NotificationConfig{Title: "x", Variant: "succss"})
}

func TestNotificationDangerGetsAlertRole(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "x", Variant: StatusDanger}))
	if !strings.Contains(h, `role="alert"`) {
		t.Errorf("expected role=alert for danger, got: %s", h)
	}
	// role=alert implies assertive; stating aria-live beside it is
	// how a message ends up announced twice.
	if strings.Contains(h, `aria-live=`) {
		t.Errorf("danger notification carries aria-live beside role=alert, announcing twice, got: %s", h)
	}
	if !strings.Contains(h, "fui-notification--danger") {
		t.Errorf("expected danger variant, got: %s", h)
	}
}

func TestNotificationVariantsRenderClass(t *testing.T) {
	for _, v := range []StatusVariant{StatusSuccess, StatusWarning, StatusInfo, StatusNeutral} {
		h := string(Notification(NotificationConfig{Title: "x", Variant: v}))
		want := "fui-notification--" + string(v)
		if v == StatusNeutral {
			// Neutral maps to the info tone on the toast primitive;
			// the sheet keeps its neutral class through the variant.
			want = "fui-notification--info"
		}
		if !strings.Contains(h, want) {
			t.Errorf("expected %s, got: %s", want, h)
		}
	}
}

func TestNotificationDismissLink(t *testing.T) {
	h := string(Notification(NotificationConfig{
		Title: "Saved", Variant: StatusSuccess, DismissHref: "/notif/dismiss/123",
		Island: headless.Island{Endpoint: "/island/notifications", Signal: "notifications"},
	}))
	for _, want := range []string{
		`href="/notif/dismiss/123"`,
		`aria-label="Dismiss notification"`,
		"fui-notification__dismiss",
	} {
		_ = want
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestNotificationOmitsDismissWhenNoHref(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "x"}))
	if strings.Contains(h, "fui-notification__dismiss") {
		t.Errorf("expected no dismiss link, got: %s", h)
	}
}

func TestNotificationBodyRenders(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "Saved", Body: "All changes persisted."}))
	if !strings.Contains(h, "All changes persisted.") {
		t.Errorf("expected body text, got: %s", h)
	}
}

func TestNotificationPositionAddsFloatingClasses(t *testing.T) {
	cases := map[NotificationPosition]string{
		NotificationTopRight:    "fui-notification--at-top-right",
		NotificationTopLeft:     "fui-notification--at-top-left",
		NotificationBottomRight: "fui-notification--at-bottom-right",
		NotificationBottomLeft:  "fui-notification--at-bottom-left",
	}
	for pos, want := range cases {
		h := string(Notification(NotificationConfig{Title: "x", Position: pos}))
		if !strings.Contains(h, "fui-notification--floating") {
			t.Errorf("Position=%q expected floating class, got: %s", pos, h)
		}
		if !strings.Contains(h, want) {
			t.Errorf("Position=%q expected %s, got: %s", pos, want, h)
		}
	}
}

func TestNotificationInlineHasNoFloatingClass(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "x"}))
	if strings.Contains(h, "fui-notification--floating") {
		t.Errorf("default (inline) should not be floating, got: %s", h)
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

func TestNotificationExtraAttrsOnRoot(t *testing.T) {
	h := Notification(NotificationConfig{
		Title:      "Saved",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("notification root missing data-test:\n%s", root)
	}
}
