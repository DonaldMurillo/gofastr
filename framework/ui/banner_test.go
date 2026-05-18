package ui

import (
	"strings"
	"testing"
)

func TestBannerRequiresTitle(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Banner without Title should panic")
		}
	}()
	Banner(BannerConfig{})
}

func TestBannerRejectsUnknownVariant(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Banner with unknown Variant should panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "bogus") {
			t.Errorf("panic should name the bogus variant: %q", msg)
		}
	}()
	Banner(BannerConfig{Title: "x", Variant: BannerVariant("bogus")})
}

func TestBannerVariantsRoleSemantics(t *testing.T) {
	// Info / Success → role=status (polite)
	info := string(Banner(BannerConfig{Title: "x", Variant: BannerInfo}))
	if !strings.Contains(info, `role="status"`) {
		t.Errorf("info banner should be role=status:\n%s", info)
	}
	if !strings.Contains(info, `aria-live="polite"`) {
		t.Errorf("info banner should be aria-live=polite:\n%s", info)
	}

	success := string(Banner(BannerConfig{Title: "x", Variant: BannerSuccess}))
	if !strings.Contains(success, `role="status"`) {
		t.Errorf("success banner should be role=status:\n%s", success)
	}

	// Warn / Danger → role=alert (assertive, interrupts)
	warn := string(Banner(BannerConfig{Title: "x", Variant: BannerWarn}))
	if !strings.Contains(warn, `role="alert"`) {
		t.Errorf("warn banner should be role=alert:\n%s", warn)
	}
	danger := string(Banner(BannerConfig{Title: "x", Variant: BannerDanger}))
	if !strings.Contains(danger, `role="alert"`) {
		t.Errorf("danger banner should be role=alert:\n%s", danger)
	}
}

func TestBannerDismissibleEmitsButtonAndMarker(t *testing.T) {
	h := string(Banner(BannerConfig{
		Title: "x", Dismissible: true, DismissID: "feature-X-2026",
	}))
	if !strings.Contains(h, "data-fui-banner-dismiss") {
		t.Errorf("Dismissible should emit data-fui-banner-dismiss:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-banner-dismiss-id="feature-X-2026"`) {
		t.Errorf("DismissID should emit data-fui-banner-dismiss-id:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Dismiss"`) {
		t.Errorf("dismiss button should have aria-label=Dismiss:\n%s", h)
	}
}

func TestBannerNotDismissibleByDefault(t *testing.T) {
	h := string(Banner(BannerConfig{Title: "x"}))
	if strings.Contains(h, "data-fui-banner-dismiss") {
		t.Errorf("default Banner should NOT be dismissible:\n%s", h)
	}
}

func TestBannerActionRenders(t *testing.T) {
	h := string(Banner(BannerConfig{
		Title:  "Heads up",
		Action: Link(LinkConfig{Href: "/x", Text: "Go"}),
	}))
	if !strings.Contains(h, "ui-banner__action") {
		t.Errorf("Action should render in .ui-banner__action wrapper:\n%s", h)
	}
	if !strings.Contains(h, `href="/x"`) {
		t.Errorf("Action HTML should appear in output:\n%s", h)
	}
}
