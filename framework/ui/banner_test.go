package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
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
	// The SystemBanner posture: a system message is polite even when
	// urgent — it sits at the top of the shell and must not interrupt
	// (the offline banner is the one exception, and it is the
	// NetworkRetryBanner's, not this component's).
	if strings.Contains(info, `role="alert"`) {
		t.Errorf("an info banner must not interrupt:\n%s", info)
	}

	success := string(Banner(BannerConfig{Title: "x", Variant: BannerSuccess}))
	if !strings.Contains(success, `role="status"`) {
		t.Errorf("success banner should be role=status:\n%s", success)
	}

	// Warn / Danger keep the polite posture and carry the urgency in
	// the tone word said before the title — the primitive's contract.
	warn := string(Banner(BannerConfig{Title: "x", Variant: BannerWarn}))
	if !strings.Contains(warn, `role="status"`) {
		t.Errorf("warn banner should stay polite (the tone word carries the urgency):\n%s", warn)
	}
	if !strings.Contains(warn, "Warning: ") {
		t.Errorf("warn banner should say the tone word:\n%s", warn)
	}
	danger := string(Banner(BannerConfig{Title: "x", Variant: BannerDanger}))
	if !strings.Contains(danger, "Error: ") {
		t.Errorf("danger banner should say the tone word:\n%s", danger)
	}
}

func TestBannerDismissibleEmitsButtonAndMarker(t *testing.T) {
	h := string(Banner(BannerConfig{
		Title: "x", Dismissible: true, DismissID: "feature-X-2026",
	}))
	if !strings.Contains(h, "data-hui-system-dismiss") {
		t.Errorf("Dismissible should emit the system dismiss hook:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-system-id="feature-X-2026"`) {
		t.Errorf("DismissID is the message's identity on the primitive:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Dismiss"`) {
		t.Errorf("dismiss button should have aria-label=Dismiss:\n%s", h)
	}
}

func TestBannerNotDismissibleByDefault(t *testing.T) {
	h := string(Banner(BannerConfig{Title: "x"}))
	if strings.Contains(h, "data-hui-system-dismiss") {
		t.Errorf("default Banner should NOT be dismissible:\n%s", h)
	}
}

func TestBannerActionRenders(t *testing.T) {
	h := string(Banner(BannerConfig{
		Title:  "Heads up",
		Action: Link(LinkConfig{Href: "/x", Text: "Go"}),
	}))
	if !strings.Contains(h, "fui-banner__action") {
		t.Errorf("Action should render in .fui-banner__action wrapper:\n%s", h)
	}
	if !strings.Contains(h, `href="/x"`) {
		t.Errorf("Action HTML should appear in output:\n%s", h)
	}
}

func TestBannerCSSAvoidsDecorativeSideStripe(t *testing.T) {
	css := bannerCSS(style.Theme{})
	for _, banned := range []string{"border-left:", "border-left-color"} {
		if strings.Contains(css, banned) {
			t.Errorf("bannerCSS must not use %q:\n%s", banned, css)
		}
	}
	if !strings.Contains(css, "--ui-banner-accent") {
		t.Errorf("banner variants should still provide a full-outline accent:\n%s", css)
	}
}

func TestBannerExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := Banner(BannerConfig{Title: "Heads up", ExtraAttrs: map[string]string{
		"data-test": "hook", "role": "evil", "Class": "evil",
	}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `role="status"`) {
		t.Errorf("severity role lost its framework value:\n%s", root)
	}
	for _, banned := range []string{"evil", `role="alert"`} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
}
