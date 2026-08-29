package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func mustContain(t *testing.T, h render.HTML, sub string) {
	t.Helper()
	if !strings.Contains(string(h), sub) {
		t.Fatalf("expected HTML to contain %q\ngot: %s", sub, h)
	}
}

// ─── PageHeader ───
func TestPageHeaderRequiresTitle(t *testing.T) {
	defer func() { recover() }()
	PageHeader(PageHeaderConfig{})
	t.Fatal("expected panic with empty Title")
}

func TestPageHeaderRendersTitleAndOptionalParts(t *testing.T) {
	h := PageHeader(PageHeaderConfig{
		Title:    "Customers",
		Subtitle: "1,283 active",
		Eyebrow:  "Admin",
		Actions:  render.Text("ACTIONS_SLOT"),
	})
	for _, want := range []string{"Customers", "1,283 active", "Admin", "ACTIONS_SLOT",
		"ui-page-header", "ui-page-header__eyebrow", "ui-page-header__actions"} {
		mustContain(t, h, want)
	}
}

func TestPageHeaderOmitsActionsWhenEmpty(t *testing.T) {
	h := PageHeader(PageHeaderConfig{Title: "x"})
	if strings.Contains(string(h), "ui-page-header__actions") {
		t.Fatal("expected no actions div when Actions is empty")
	}
}

// ─── Section ───
func TestSectionRendersHeadingDescriptionBody(t *testing.T) {
	h := Section(SectionConfig{Heading: "Settings", Description: "Account-wide"},
		render.Text("BODY"))
	for _, want := range []string{"Settings", "Account-wide", "BODY", "ui-section__body"} {
		mustContain(t, h, want)
	}
}

func TestSectionEyebrowRendersBeforeHeadingAndIsDecorative(t *testing.T) {
	h := Section(SectionConfig{
		Eyebrow: "01 / what it generates",
		Heading: "One entity call",
	}, render.Text("BODY"))
	s := string(h)
	mustContain(t, h, "ui-section__eyebrow")
	mustContain(t, h, "01 / what it generates")
	// Decorative numeric eyebrow, hidden from the a11y tree so SR users
	// don't hear "01 slash what it generates" then the heading.
	mustContain(t, h, `aria-hidden="true"`)
	eyebrowIdx := strings.Index(s, "ui-section__eyebrow")
	headingIdx := strings.Index(s, "ui-section__heading")
	if eyebrowIdx == -1 || headingIdx == -1 || eyebrowIdx > headingIdx {
		t.Errorf("eyebrow must render before heading in source order:\n%s", s)
	}
}

func TestSectionDescriptionHTMLOverridesDescription(t *testing.T) {
	h := Section(SectionConfig{
		Heading:         "Forms",
		Description:     "plain",
		DescriptionHTML: render.Raw(`lede with <code>.gofastr/</code>`),
	}, render.Text("BODY"))
	mustContain(t, h, `<code>.gofastr/</code>`)
	if strings.Contains(string(h), ">plain<") {
		t.Errorf("DescriptionHTML should win over Description:\n%s", h)
	}
}

func TestSectionLabelUsedWhenNoHeading(t *testing.T) {
	h := Section(SectionConfig{Label: "State of the project"}, render.Text("BODY"))
	mustContain(t, h, `aria-label="State of the project"`)
	if strings.Contains(string(h), `aria-label="Section"`) {
		t.Errorf("explicit Label should replace the generic fallback:\n%s", h)
	}
}

// ─── FormField ───
func TestFormFieldRequiresLabelForInput(t *testing.T) {
	defer func() { recover() }()
	FormField(FormFieldConfig{})
	t.Fatal("expected panic on empty config")
}

func TestFormFieldRequired(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "name"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "name", Required: true, Input: in,
	})
	mustContain(t, h, `for="name"`)
	mustContain(t, h, "Name")
	mustContain(t, h, "ui-form-field__required")
}

func TestFormFieldErrorSwitchesStyling(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Error: "Required field", Input: in,
		Help: "Your legal name",
	})
	mustContain(t, h, "is-error")
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Required field")
	// Help text should also be present alongside error (S-3).
	mustContain(t, h, "ui-form-field__help")
	mustContain(t, h, "Your legal name")
}

func TestFormFieldHelpRendersWhenNoError(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{Label: "x", For: "n", Help: "Hint", Input: in})
	mustContain(t, h, "Hint")
	mustContain(t, h, "ui-form-field__help")
}

func TestFormFieldHelpRendersAlongsideError(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Input: in,
		Help:  "Enter your full name",
		Error: "Required",
	})
	s := string(h)
	if !strings.Contains(s, "Enter your full name") {
		t.Errorf("help text should still render when error is present, got: %s", s)
	}
	if !strings.Contains(s, "ui-form-field__help") {
		t.Errorf("help class should still be present, got: %s", s)
	}
	if !strings.Contains(s, "Required") {
		t.Errorf("error text should render, got: %s", s)
	}
}

// ─── FormField a11y ───
func TestFormFieldErrorAddsAriaInvalid(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Error: "Required", Input: in,
	})
	s := string(h)
	if !strings.Contains(s, `aria-invalid="true"`) {
		t.Errorf("error-state FormField must add aria-invalid:\n%s", s)
	}
	if !strings.Contains(s, `aria-describedby="n-error"`) {
		t.Errorf("error-state FormField must link to error message via aria-describedby:\n%s", s)
	}
}

func TestInjectAttrsHandlesLeadingComment(t *testing.T) {
	// Input wrapped in an HTML comment must not splice into the
	// comment terminator. The attrs land on the real <input>.
	in := render.HTML(`<!-- preset --><input type="text" name="n" id="n">`)
	out := string(injectAttrs(in, ` aria-invalid="true"`))
	if !strings.Contains(out, `<input type="text" name="n" id="n" aria-invalid="true">`) {
		t.Errorf("injectAttrs should splice into the real <input> tag, not the comment:\n%s", out)
	}
	if strings.Contains(out, `comment --aria-invalid`) {
		t.Errorf("injectAttrs corrupted the comment:\n%s", out)
	}
}

func TestInjectAttrsHandlesLeadingWhitespace(t *testing.T) {
	in := render.HTML("\n  <input type=\"text\" name=\"n\">")
	out := string(injectAttrs(in, ` aria-invalid="true"`))
	if !strings.Contains(out, `aria-invalid="true"`) {
		t.Errorf("injectAttrs missed the input after whitespace:\n%s", out)
	}
}

func TestFormFieldHelpAddsAriaDescribedBy(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Help: "Use your full name.", Input: in,
	})
	s := string(h)
	if !strings.Contains(s, `aria-describedby="n-help"`) {
		t.Errorf("help-state FormField must link to help text via aria-describedby:\n%s", s)
	}
}

// ─── Button (typed variants) ───
func TestButtonVariantsRenderClass(t *testing.T) {
	for _, v := range []ButtonVariant{ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost} {
		h := Button(ButtonConfig{Label: "Action", Variant: v})
		want := "ui-button--" + string(v)
		mustContain(t, h, want)
		mustContain(t, h, "Action")
	}
}

func TestButtonDefaultsToPrimary(t *testing.T) {
	h := Button(ButtonConfig{Label: "x"})
	mustContain(t, h, "ui-button--primary")
}

func TestButtonRejectsUnknownVariant(t *testing.T) {
	// String-typed const enums don't prevent arbitrary string
	// values at the call site. The framework validates at render
	// time so a typo like ButtonVariant("tertiary") panics with a
	// useful message instead of silently rendering an unstyled
	// button.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Button with unknown Variant should panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic was %T, want string: %v", r, r)
		}
		if !strings.Contains(msg, "tertiary") {
			t.Errorf("panic message should name the bogus variant: %q", msg)
		}
	}()
	Button(ButtonConfig{Label: "Save", Variant: ButtonVariant("tertiary")})
}

// Button{Variant: ButtonDanger} must emit ONE data-fui-comp marker
// (ui-button), not two. The legacy dangerButtonStyle was wrapping
// the same element with its own marker, causing two scoped CSS files
// to ship and compete via specificity. Variant class alone handles it.
func TestButtonDangerEmitsSingleMarker(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "Delete", Variant: ButtonDanger}))
	count := strings.Count(h, "data-fui-comp=")
	if count != 1 {
		t.Errorf("Button{Variant: ButtonDanger} should emit exactly 1 data-fui-comp marker, got %d in:\n%s", count, h)
	}
	if !strings.Contains(h, `data-fui-comp="ui-button"`) {
		t.Errorf("Button{Variant: ButtonDanger} should mark as ui-button (not ui-button-danger):\n%s", h)
	}
}

func TestButtonSizeDefaultEmitsNoSizeClass(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "x"}))
	if strings.Contains(h, "ui-button--small") || strings.Contains(h, "ui-button--large") {
		t.Errorf("default Size should not emit a size modifier:\n%s", h)
	}
}

func TestButtonSizeSmallEmitsSmallClass(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "x", Size: ButtonSizeSmall}))
	if !strings.Contains(h, "ui-button--small") {
		t.Errorf("Size: ButtonSizeSmall should emit .ui-button--small:\n%s", h)
	}
}

func TestButtonSizeLargeEmitsLargeClass(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "x", Size: ButtonSizeLarge}))
	if !strings.Contains(h, "ui-button--large") {
		t.Errorf("Size: ButtonSizeLarge should emit .ui-button--large:\n%s", h)
	}
}

func TestLinkButtonRendersAnchorWithButtonClass(t *testing.T) {
	h := string(LinkButton(LinkButtonConfig{Label: "Get started", Href: "/get-started"}))
	if !strings.Contains(h, `<a `) {
		t.Errorf("LinkButton should render <a>:\n%s", h)
	}
	if !strings.Contains(h, `href="/get-started"`) {
		t.Errorf("LinkButton should preserve Href:\n%s", h)
	}
	if !strings.Contains(h, "ui-button ui-button--primary") {
		t.Errorf("LinkButton should default to primary variant:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-comp="ui-button"`) {
		t.Errorf("LinkButton should share ui-button marker for CSS scope:\n%s", h)
	}
}

func TestLinkButtonExternalAddsTargetAndRel(t *testing.T) {
	h := string(LinkButton(LinkButtonConfig{Label: "Repo", Href: "https://github.com/x", External: true}))
	if !strings.Contains(h, `target="_blank"`) || !strings.Contains(h, `rel="noopener noreferrer"`) {
		t.Errorf("LinkButton{External:true} missing target/rel:\n%s", h)
	}
}

func TestLinkButtonRefusesUnsafeSchemes(t *testing.T) {
	bad := []string{
		"javascript:alert(1)",
		"  javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"vbscript:msg",
		"data:text/html,<script>alert(1)</script>",
		"data:application/javascript,alert(1)",
	}
	for _, href := range bad {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("LinkButton must panic on unsafe Href %q", href)
				}
			}()
			LinkButton(LinkButtonConfig{Label: "x", Href: href})
		}()
	}
	// Allowed: http(s), relative paths, mailto, tel, data:image/*.
	ok := []string{"/docs/", "https://gh", "mailto:a@b", "tel:+1", "data:image/png;base64,xx"}
	for _, href := range ok {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("LinkButton must accept safe Href %q, panicked: %v", href, r)
				}
			}()
			LinkButton(LinkButtonConfig{Label: "x", Href: href})
		}()
	}
}

func TestLinkButtonRequiresHref(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("LinkButton with empty Href should panic")
		}
	}()
	LinkButton(LinkButtonConfig{Label: "x"})
}

func TestButtonRejectsUnknownSize(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Button with unknown Size should panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "huge") {
			t.Errorf("panic should name the bogus size: %q", msg)
		}
	}()
	Button(ButtonConfig{Label: "x", Size: ButtonSize("huge")})
}

// ─── StatusBadge ───
func TestStatusBadgeVariantsRenderClass(t *testing.T) {
	for _, v := range []StatusVariant{StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral} {
		h := StatusBadge(StatusBadgeConfig{Label: "x", Variant: v})
		want := "ui-badge--" + string(v)
		mustContain(t, h, want)
	}
}

func TestStatusBadgeDefaultsToNeutral(t *testing.T) {
	h := StatusBadge(StatusBadgeConfig{Label: "x"})
	mustContain(t, h, "ui-badge--neutral")
}

// TestStatusBadgeRejectsUnknownVariant mirrors Button. A typo like
// "succes" must panic instead of silently emitting an unmatched class.
func TestStatusBadgeRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for unknown StatusBadge Variant, got none")
		}
	}()
	_ = StatusBadge(StatusBadgeConfig{Label: "x", Variant: "succes"})
}

// ─── EmptyState ───
func TestEmptyStateRendersTitleDescriptionAction(t *testing.T) {
	h := EmptyState(EmptyStateConfig{
		Title: "No customers yet", Description: "Invite your first.",
		Action: render.Text("INVITE_BUTTON"),
	})
	for _, want := range []string{"No customers yet", "Invite your first.", "INVITE_BUTTON",
		"ui-empty-state__action"} {
		mustContain(t, h, want)
	}
}

// TestEmptyStateHeadingLevel verifies the title's heading level follows
// HeadingLevel (default h3; a real page under an <h1> passes 2 to avoid an
// h1→h3 skip). Guards the admin list empty-state fix.
func TestEmptyStateHeadingLevel(t *testing.T) {
	if h := string(EmptyState(EmptyStateConfig{Title: "x"})); !strings.Contains(h, "<h3") {
		t.Fatalf("default EmptyState title should be <h3>; got %s", h)
	}
	if h := string(EmptyState(EmptyStateConfig{Title: "x", HeadingLevel: 2})); !strings.Contains(h, "<h2") {
		t.Fatalf("HeadingLevel: 2 should render <h2>; got %s", h)
	}
}

// ─── Callout ───
// TestCalloutRejectsUnknownVariant mirrors Button/StatusBadge. Typo
// must panic instead of silently emitting an unmatched class.
// TestCalloutCSSAvoidsSideStripe. Design ban: colored side-stripe
// borders on cards/list items/callouts are a recognizable AI/SaaS
// template tell. The Callout variant cue must come from a surface
// tint + a leading icon glyph, never from a `border-inline-start`
// width or color override. Regression guard for the redesign.
func TestCalloutCSSAvoidsSideStripe(t *testing.T) {
	css := calloutCSS(style.Theme{})
	for _, banned := range []string{
		"border-inline-start-width",
		"border-inline-start-color",
		"border-left-width",
		"border-left:",
	} {
		if strings.Contains(css, banned) {
			t.Errorf("calloutCSS must not use %q (side-stripe ban):\n%s", banned, css)
		}
	}
	// Positive: variant signaling routes through the --ui-callout-accent
	// custom property + the leading ::before icon glyph.
	for _, want := range []string{
		"--ui-callout-accent",
		"::before",
		"--ui-callout-icon",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("calloutCSS missing variant-cue hook %q", want)
		}
	}
}

func TestCalloutRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for unknown Callout Variant, got none")
		}
	}()
	_ = Callout(CalloutConfig{Variant: "succes"}, render.Text("hi"))
}

func TestCalloutRoleSwitchesForAlerts(t *testing.T) {
	// Danger/warning callouts must announce assertively → role=alert
	// (rendered as a <div role="alert">).
	for _, v := range []StatusVariant{StatusDanger, StatusWarning} {
		h := Callout(CalloutConfig{Title: "x", Variant: v}, render.Text("body"))
		mustContain(t, h, `role="alert"`)
	}
	// Info/success/neutral callouts are non-urgent → rendered as
	// <aside role="complementary"> (via html.Aside) so screen
	// readers treat them as side notes.
	for _, v := range []StatusVariant{StatusInfo, StatusSuccess, StatusNeutral} {
		h := Callout(CalloutConfig{Title: "x", Variant: v}, render.Text("body"))
		mustContain(t, h, `<aside`)
		mustContain(t, h, `role="complementary"`)
	}
}

// TestCalloutLandmarkOptOut verifies the Landmark=false config renders an
// inline callout as a plain <div> (not a complementary <aside>), so it can
// nest inside <main> without tripping landmark-complementary-is-top-level.
// Default (nil) keeps the <aside> landmark.
func TestCalloutLandmarkOptOut(t *testing.T) {
	noLandmark := false
	h := Callout(CalloutConfig{Title: "Tip", Variant: StatusInfo, Landmark: &noLandmark}, render.Text("body"))
	if strings.Contains(string(h), `<aside`) || strings.Contains(string(h), `role="complementary"`) {
		t.Errorf("Landmark=false should render a <div>, not a complementary <aside>:\n%s", h)
	}
	if !strings.Contains(string(h), `ui-callout--info`) {
		t.Errorf("Landmark=false should keep the variant styling:\n%s", h)
	}
	// Default still renders the complementary landmark.
	def := Callout(CalloutConfig{Title: "Tip", Variant: StatusInfo}, render.Text("body"))
	mustContain(t, def, `<aside`)
	mustContain(t, def, `role="complementary"`)
}

// ─── StatCard ───
func TestStatCardRequiresLabelAndValue(t *testing.T) {
	defer func() { recover() }()
	StatCard(StatCardConfig{Label: "x"})
	t.Fatal("expected panic when Value missing")
}

func TestStatCardTrendDirection(t *testing.T) {
	h := StatCard(StatCardConfig{Label: "Revenue", Value: "$12.4k", Trend: "+8%", Direction: TrendUp})
	mustContain(t, h, "ui-stat-card__trend--up")
}

// ─── Avatar ───
func TestAvatarFallsBackToInitials(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Donald Murillo"})
	mustContain(t, h, "DM")
	mustContain(t, h, "ui-avatar__initials")
}

func TestAvatarUsesImageWhenSrcSet(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Alice", Src: "/avatars/alice.png"})
	mustContain(t, h, `src="/avatars/alice.png"`)
	mustContain(t, h, `alt="Alice"`)
}

func TestAvatarSizeVariantClass(t *testing.T) {
	cases := map[AvatarSize]string{
		AvatarSm: "ui-avatar--sm",
		AvatarLg: "ui-avatar--lg",
		AvatarXl: "ui-avatar--xl",
	}
	for size, want := range cases {
		h := Avatar(AvatarConfig{Name: "x", Size: size})
		mustContain(t, h, want)
	}
	// Default size: no variant class, but the base class is there.
	h := Avatar(AvatarConfig{Name: "x"})
	mustContain(t, h, "class=\"ui-avatar\"")
}

func TestInitialsHelper(t *testing.T) {
	cases := map[string]string{
		"Donald Murillo": "DM",
		"alice":          "A",
		"three name foo": "TF",
		"":               "",
	}
	for in, want := range cases {
		got := initials(in)
		if got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}

// injectAriaInvalid must escape the errID to prevent attribute injection
// when cfg.For contains special characters (quotes, angle brackets).
func TestInjectAriaInvalidEscapesID(t *testing.T) {
	input := render.HTML(`<input id="test" name="test">`)
	result := string(injectAriaInvalid(input, `foo"bar`))
	// The raw quote must be escaped, not break the attribute boundary.
	if strings.Contains(result, `aria-describedby="foo"bar"`) {
		t.Errorf("unescaped ID in aria-describedby — attribute injection:\n%s", result)
	}
	if !strings.Contains(result, `aria-invalid="true"`) {
		t.Errorf("missing aria-invalid:\n%s", result)
	}
}

// injectAttrs must inject aria-describedby even when aria-invalid is
// already present on the element. Idempotence check must cover all attrs.
func TestInjectAttrsDoesNotSkipDescribedByWhenInvalidPresent(t *testing.T) {
	input := render.HTML(`<input id="test" aria-invalid="true">`)
	result := string(injectAttrs(input, ` aria-invalid="true" aria-describedby="test-error"`))
	if !strings.Contains(result, `aria-describedby="test-error"`) {
		t.Errorf("aria-describedby was skipped because aria-invalid already present:\n%s", result)
	}
}

// ─── ExtraAttrs pass-through (#251) ───

func TestPageHeaderExtraAttrsOnRoot(t *testing.T) {
	h := PageHeader(PageHeaderConfig{Title: "x", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("PageHeader root missing data-test:\n%s", root)
	}
}

func TestSectionExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"heading": Section(SectionConfig{Heading: "x", ExtraAttrs: extra}, render.Text("b")),
		"label":   Section(SectionConfig{Label: "y", ExtraAttrs: extra}, render.Text("b")),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

func TestFormFieldExtraAttrsOnRoot(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Name", For: "f",
		Input:      html.Input(html.InputConfig{Type: "text", Name: "f", ID: "f"}),
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("FormField root missing data-test:\n%s", root)
	}
}

func TestFormSectionExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"div":      FormSection(FormSectionConfig{ExtraAttrs: extra}, render.Text("f")),
		"fieldset": FormSection(FormSectionConfig{Heading: "h", ExtraAttrs: extra}, render.Text("f")),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

func TestStatusBadgeExtraAttrsOnRoot(t *testing.T) {
	h := StatusBadge(StatusBadgeConfig{Label: "ok", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("StatusBadge root missing data-test:\n%s", root)
	}
}

func TestEmptyStateExtraAttrsOnRoot(t *testing.T) {
	h := EmptyState(EmptyStateConfig{Title: "No items", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("EmptyState root missing data-test:\n%s", root)
	}
}

func TestCalloutExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	inline := false
	for name, h := range map[string]render.HTML{
		"aside": Callout(CalloutConfig{Title: "t", ExtraAttrs: extra}, render.Text("b")),
		"alert": Callout(CalloutConfig{Variant: StatusDanger, ExtraAttrs: extra}, render.Text("b")),
		"div":   Callout(CalloutConfig{Landmark: &inline, ExtraAttrs: extra}, render.Text("b")),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

func TestStatCardExtraAttrsOnRoot(t *testing.T) {
	h := StatCard(StatCardConfig{Label: "l", Value: "1", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("StatCard root missing data-test:\n%s", root)
	}
}

func TestAvatarExtraAttrsOnRoot(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Ada Lovelace", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Avatar root missing data-test:\n%s", root)
	}
}

func TestCodeBlockExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"pre":    CodeBlock(CodeBlockConfig{Code: "x = 1", ExtraAttrs: extra}),
		"framed": CodeBlock(CodeBlockConfig{Code: "x = 1", Filename: "a.go", ExtraAttrs: extra}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

func TestCodeBlockExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := CodeBlock(CodeBlockConfig{
		Code: "x", Language: "go",
		ExtraAttrs: map[string]string{
			"tabindex": "9", "ARIA-LABEL": "evil", "data-fui-comp": "spoof",
		},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	for _, banned := range []string{`tabindex="9"`, `evil`, `spoof`} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	mustContain(t, h, `tabindex="0"`)
	mustContain(t, h, `aria-label="go source"`)
}

func TestSkipLinkExtraAttrsOnRoot(t *testing.T) {
	h := SkipLink(SkipLinkConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("SkipLink root missing data-test:\n%s", root)
	}
}

func TestButtonExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := Button(ButtonConfig{Label: "Save", ExtraAttrs: map[string]string{
		"data-test": "hook", "type": "evil", "Class": "evil",
		"aria-label": "evil", "data-fui-comp": "evil",
	}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `type="button"`) {
		t.Errorf("button type lost its framework value:\n%s", root)
	}
	if strings.Contains(root, "evil") {
		t.Errorf("owned attr overridden by ExtraAttrs:\n%s", root)
	}
}

func TestButtonAriaLabelOverridesVisibleLabel(t *testing.T) {
	// #281: several buttons sharing a visible Label need distinct
	// accessible names. AriaLabel is the supported override; an
	// aria-label in ExtraAttrs stays dropped (owned key).
	h := string(Button(ButtonConfig{
		Label:      "Revoke",
		AriaLabel:  "Revoke admin from Alice",
		ExtraAttrs: map[string]string{"aria-label": "ignored"},
	}))
	root := h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `aria-label="Revoke admin from Alice"`) {
		t.Errorf("AriaLabel must win as the accessible name:\n%s", root)
	}
	if strings.Contains(root, "ignored") {
		t.Errorf("ExtraAttrs aria-label must stay dropped:\n%s", root)
	}
	// Visible text is still Label, not AriaLabel.
	if !strings.Contains(h, `>Revoke</button>`) {
		t.Errorf("visible text must remain Label:\n%s", h)
	}
}

func TestButtonExtraAttrsCarriesWiring(t *testing.T) {
	// Button is the documented carrier for interactive wiring
	// (interactive-patterns.md attaches Action.Attrs() via ExtraAttrs):
	// data-fui-* must pass through, unlike components that own their
	// own wiring. framework/ui/resource and battery/admin depend on it.
	h := Button(ButtonConfig{Label: "Delete", ExtraAttrs: map[string]string{
		"data-fui-rpc":        "/api/items/42",
		"data-fui-rpc-method": "DELETE",
		"data-fui-confirm":    "Delete this item?",
	}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	for _, want := range []string{
		`data-fui-rpc="/api/items/42"`,
		`data-fui-rpc-method="DELETE"`,
		`data-fui-confirm="Delete this item?"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("wiring attr %s dropped from carrier button:\n%s", want, root)
		}
	}
}

func TestLinkButtonExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := LinkButton(LinkButtonConfig{Label: "Go", Href: "/real", ExtraAttrs: map[string]string{
		"data-test": "hook", "href": "javascript:alert(1)", "Class": "evil",
	}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `href="/real"`) {
		t.Errorf("framework href lost:\n%s", root)
	}
	for _, banned := range []string{"evil", "javascript:"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
}

func TestLinkButtonExternalDropsCaseVariantTargetRel(t *testing.T) {
	// "TARGET"/"REL" survive a lowercase-only overwrite as distinct map
	// keys, sort BEFORE the owned lowercase attrs in the rendered tag,
	// and first-occurrence-wins in the parser — so without protection a
	// case-variant clobbers External's noopener contract.
	h := LinkButton(LinkButtonConfig{
		Label: "Docs", Href: "https://example.com", External: true,
		ExtraAttrs: map[string]string{"TARGET": "evil", "REL": "evil"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if strings.Contains(root, "evil") {
		t.Errorf("case-variant target/rel survived on an External link:\n%s", root)
	}
	for _, want := range []string{`target="_blank"`, `rel="noopener noreferrer"`} {
		if !strings.Contains(root, want) {
			t.Errorf("External link missing %s:\n%s", want, root)
		}
	}
}

func TestLinkButtonExternalOwnsTargetAndRel(t *testing.T) {
	// External must win even when extras try to set target/rel — and
	// the all-dropped (nil-map) branch must still emit both.
	h := LinkButton(LinkButtonConfig{
		Label: "Docs", Href: "https://example.com", External: true,
		ExtraAttrs: map[string]string{"target": "evil", "rel": "evil"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `target="_blank"`) || !strings.Contains(root, `rel="noopener noreferrer"`) {
		t.Errorf("External target/rel lost:\n%s", root)
	}
	if strings.Contains(root, "evil") {
		t.Errorf("ExtraAttrs target/rel overrode External:\n%s", root)
	}
}
