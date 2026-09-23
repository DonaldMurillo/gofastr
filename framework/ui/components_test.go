package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
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
		`"fui-page-header`, `"fui-page-header__eyebrow`, `"fui-page-header__actions`} {
		mustContain(t, h, want)
	}
}

func TestPageHeaderOmitsActionsWhenEmpty(t *testing.T) {
	h := PageHeader(PageHeaderConfig{Title: "x"})
	if strings.Contains(string(h), `"fui-page-header__actions"`) {
		t.Fatal("expected no actions div when Actions is empty")
	}
}

// ─── Section ───
func TestSectionRendersHeadingDescriptionBody(t *testing.T) {
	h := Section(SectionConfig{Heading: "Settings", Description: "Account-wide"},
		render.Text("BODY"))
	for _, want := range []string{"Settings", "Account-wide", "BODY", `"fui-section__body`} {
		mustContain(t, h, want)
	}
}

func TestSectionEyebrowRendersBeforeHeadingAndIsDecorative(t *testing.T) {
	h := Section(SectionConfig{
		Eyebrow: "01 / what it generates",
		Heading: "One entity call",
	}, render.Text("BODY"))
	s := string(h)
	mustContain(t, h, `"fui-section__eyebrow`)
	mustContain(t, h, "01 / what it generates")
	// Decorative numeric eyebrow, hidden from the a11y tree so SR users
	// don't hear "01 slash what it generates" then the heading.
	mustContain(t, h, `aria-hidden="true"`)
	eyebrowIdx := strings.Index(s, `"fui-section__eyebrow`)
	headingIdx := strings.Index(s, `"fui-section__heading`)
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

// A nil Input is the one misuse the type cannot prevent: the builder
// is the point, and a missing one is a migration half-done.
func TestFormFieldRequiresInputBuilder(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil Input")
		}
		if !strings.Contains(r.(string), "builder") {
			t.Fatalf("the panic should say what Input now is, got: %v", r)
		}
	}()
	FormField(FormFieldConfig{Label: "n", For: "n"})
}

func TestFormFieldRequired(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Name", For: "name", Required: true,
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "n"})
		},
	})
	mustContain(t, h, `for="name"`)
	mustContain(t, h, "Name")
	// The required mark is drawn from the state the label carries.
	mustContain(t, h, `data-required`)
}

func TestFormFieldErrorSwitchesStyling(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Error: "Required field",
		Help: "Your legal name",
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "n"})
		},
	})
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Required field")
	// Help text is present alongside the error, after it.
	mustContain(t, h, "fui-field__hint")
	mustContain(t, h, "Your legal name")
}

func TestFormFieldHelpRendersWhenNoError(t *testing.T) {
	h := FormField(FormFieldConfig{Label: "x", For: "n", Help: "Hint",
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "n"})
		}})
	mustContain(t, h, "Hint")
	mustContain(t, h, "fui-field__hint")
}

// The both-visible contract, order included: the error paragraph
// precedes the hint, and the control's described-by lists the error's
func TestFormFieldHelpRendersAlongsideError(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n",
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "n"})
		},
		Help:  "Enter your full name",
		Error: "Required",
	})
	s := string(h)
	errAt := strings.Index(s, `id="n-error"`)
	hintAt := strings.Index(s, `id="n-hint"`)
	// A missing node indexes at -1 and -1 compares as "in order", so
	// absence fails first, before the order comparison runs.
	if errAt == -1 || hintAt == -1 {
		t.Fatalf("the error node or the hint node is missing (error at %d, hint at %d):\n%s", errAt, hintAt, s)
	}
	if errAt > hintAt {
		t.Errorf("the error must be drawn before the hint:\n%s", s)
	}
	if !strings.Contains(s, `aria-describedby="n-error n-hint"`) {
		t.Errorf("the control must carry both ids, error first:\n%s", s)
	}
	if !strings.Contains(s, "Required") || !strings.Contains(s, "Enter your full name") {
		t.Errorf("both messages must render:\n%s", s)
	}
}

// ─── FormField a11y ───
func TestFormFieldErrorAddsAriaInvalid(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Error: "Required",
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "n"})
		},
	})
	s := string(h)
	if !strings.Contains(s, `aria-invalid="true"`) {
		t.Errorf("error-state FormField must add aria-invalid:\n%s", s)
	}
	if !strings.Contains(s, `aria-describedby="n-error"`) {
		t.Errorf("error-state FormField must link to error message via aria-describedby:\n%s", s)
	}
}

func TestFormFieldHelpAddsAriaDescribedBy(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Help: "Use your full name.",
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "n"})
		},
	})
	s := string(h)
	if !strings.Contains(s, `aria-describedby="n-hint"`) {
		t.Errorf("help-state FormField must link to help text via aria-describedby:\n%s", s)
	}
}

// The reserved error node: rendered empty and wired into the control's
// description, found by the id that rides aria-describedby.
func TestFormFieldReserveErrorRendersAnEmptyWiredNode(t *testing.T) {
	h := FormField(FormFieldConfig{
		Label: "Token", For: "tok", ReserveError: true,
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "tok"})
		},
	})
	s := string(h)
	if !strings.Contains(s, `id="tok-error" role="alert"></p>`) &&
		!strings.Contains(s, `role="alert" id="tok-error"></p>`) {
		t.Errorf("the reserved node must render empty, so the stylesheet can take it out of the grid until a script fills it:\n%s", s)
	}
	if strings.Contains(s, "data-hui-") {
		t.Errorf("the reserved node carries a data-hui-* hook, which belongs to a runtime module that binds it; nothing binds this one:\n%s", s)
	}
	if !strings.Contains(s, `aria-describedby="tok-error"`) {
		t.Errorf("the reserved node's id must ride the control's description:\n%s", s)
	}
	if !strings.Contains(s, `id="tok-error"`) {
		t.Errorf("the reserved node must carry the stable id:\n%s", s)
	}
}

// ─── Button (typed variants) ───
func TestButtonVariantsRenderClass(t *testing.T) {
	for _, v := range []ButtonVariant{ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost} {
		h := Button(ButtonConfig{Label: "Action", Variant: v})
		want := "fui-button--" + string(v)
		mustContain(t, h, want)
		mustContain(t, h, "Action")
	}
}

func TestButtonDefaultsToPrimary(t *testing.T) {
	h := Button(ButtonConfig{Label: "x"})
	mustContain(t, h, "fui-button--primary")
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
	if strings.Contains(h, "fui-button--small") || strings.Contains(h, "fui-button--large") {
		t.Errorf("default Size should not emit a size modifier:\n%s", h)
	}
}

func TestButtonSizeSmallEmitsSmallClass(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "x", Size: ButtonSizeSmall}))
	if !strings.Contains(h, "fui-button--small") {
		t.Errorf("Size: ButtonSizeSmall should emit .fui-button--small:\n%s", h)
	}
}

func TestButtonSizeLargeEmitsLargeClass(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "x", Size: ButtonSizeLarge}))
	if !strings.Contains(h, "fui-button--large") {
		t.Errorf("Size: ButtonSizeLarge should emit .fui-button--large:\n%s", h)
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
	if !strings.Contains(h, "fui-button fui-button--primary") {
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
	// External owns the pair: a caller's spelling must not clobber
	// the noopener contract, whichever case it arrives in.
	smuggled := string(LinkButton(LinkButtonConfig{Label: "Repo", Href: "https://github.com/x", External: true,
		ExtraAttrs: html.Attrs{"TARGET": "_self", "REL": "opener"}}))
	if strings.Contains(smuggled, "_self") || strings.Contains(smuggled, "opener\"") {
		t.Errorf("a case-variant target/rel survived External ownership:\n%s", smuggled)
	}
	// Without External the caller keeps the keys.
	caller := string(LinkButton(LinkButtonConfig{Label: "Repo", Href: "https://example.com/x",
		ExtraAttrs: html.Attrs{"target": "framename"}}))
	if !strings.Contains(caller, `target="framename"`) {
		t.Errorf("without External a caller may set target:\n%s", caller)
	}
	if strings.Contains(string(LinkButton(LinkButtonConfig{Label: "Repo", Href: "https://example.com"})), "target=") {
		t.Error("target must not appear without External or a caller setting it")
	}
}

func TestLinkButtonRefusesUnsafeSchemes(t *testing.T) {
	bad := []string{
		"javascript:alert(1)",
		"  javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"vbscript:msg",
		"data:text/html,<script>alert(1)</script>",
		"data:image/png;base64,xx",
		"data:application/javascript,alert(1)",
		// Origin-absolute spellings: a foreign origin without a
		// scheme. headless's anchor policy drops both to a dead link;
		// the panic names the mistake where it is made (finding 5).
		"//evil.example/x",
		`/\evil.example/x`,
		"/\t/evil.example/x",
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
	// Allowed: http(s), relative paths, mailto, tel. Every data: URL is
	// refused, images included, matching the anchor policy headless
	// applies: admitting one would render a dead link, not a panic.
	ok := []string{"/docs/", "https://gh", "mailto:a@b", "tel:+1"}
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
		want := ` fui-badge--` + string(v) + `"` // boundary: follows the base class
		mustContain(t, h, want)
	}
}

func TestStatusBadgeDefaultsToNeutral(t *testing.T) {
	h := StatusBadge(StatusBadgeConfig{Label: "x"})
	mustContain(t, h, ` fui-badge--neutral"`)
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
		`"fui-empty-state__action`} {
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
	// Danger/warning callouts must announce assertively → role=alert.
	for _, v := range []StatusVariant{StatusDanger, StatusWarning} {
		h := Callout(CalloutConfig{Title: "x", Variant: v}, render.Text("body"))
		mustContain(t, h, `role="alert"`)
	}
	// Info/success/neutral callouts are standing messages the page
	// rendered: no live role at all, so nothing interrupts on load.
	for _, v := range []StatusVariant{StatusInfo, StatusSuccess, StatusNeutral} {
		h := Callout(CalloutConfig{Title: "x", Variant: v}, render.Text("body"))
		if strings.Contains(string(h), "role=") {
			t.Errorf("%s: a standing callout claimed a role:\n%s", v, h)
		}
	}
	// The complementary-<aside> shape is gone: an inline tip is
	// emphasis, not a tangential region.
	h := Callout(CalloutConfig{Title: "Tip", Variant: StatusInfo}, render.Text("body"))
	if strings.Contains(string(h), "<aside") || strings.Contains(string(h), "complementary") {
		t.Errorf("the aside shape survived the move to headless.Alert:\n%s", h)
	}
	mustContain(t, h, "fui-callout--info")
}
func TestStatCardRequiresLabelAndValue(t *testing.T) {
	defer func() { recover() }()
	StatCard(StatCardConfig{Label: "x"})
	t.Fatal("expected panic when Value missing")
}

func TestStatCardTrendDirection(t *testing.T) {
	h := StatCard(StatCardConfig{Label: "Revenue", Value: "$12.4k", Trend: "+8%", Direction: TrendUp})
	// Boundary form: the variant token follows the base trend class.
	mustContain(t, h, ` fui-stat-card__trend--up"`)
	mustContain(t, h, `data-direction="up"`)
}

// ─── Avatar ───
func TestAvatarFallsBackToInitials(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Donald Murillo"})
	mustContain(t, h, "DM")
	mustContain(t, h, "fui-avatar__initials")
}

func TestAvatarUsesImageWhenSrcSet(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Alice", Src: "/avatars/alice.png"})
	mustContain(t, h, `src="/avatars/alice.png"`)
	mustContain(t, h, `alt="Alice"`)
}

func TestAvatarSizeVariantClass(t *testing.T) {
	cases := map[AvatarSize]string{
		AvatarSm: "fui-avatar--sm",
		AvatarLg: "fui-avatar--lg",
		AvatarXl: "fui-avatar--xl",
	}
	for size, want := range cases {
		h := Avatar(AvatarConfig{Name: "x", Size: size})
		mustContain(t, h, want)
	}
	// Default size: no variant class, but the base class is there.
	h := Avatar(AvatarConfig{Name: "x"})
	mustContain(t, h, "class=\"fui-avatar\"")
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

// injectAttrs and its ARIA wrappers were deleted with FormField's
// post-hoc string surgery: the builder hands the wiring down by
// construction, so there is nothing left to splice. The escaping
// those tests pinned now lives in headless's attribute renderer,
// pinned by the headless package's own tests.

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
		Input: func(c headless.FieldControl) render.HTML {
			return Control(ControlConfig{Field: c, Type: "text", Name: "f"})
		},
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

// The legend maps to the exact token the sheet styles — a heading
// class the old markup emitted, so a headed section keeps its legend
// typography, and a rule for that token exists in the sheet.
func TestFormSectionLegendCarriesTheSheetHeadingClass(t *testing.T) {
	h := FormSection(FormSectionConfig{Heading: "Access"}, render.Text("f"))
	if !strings.Contains(string(h), `<legend class="fui-form-section__heading">`) {
		t.Errorf("the legend does not carry the heading class the sheet styles:\n%s", h)
	}
	css := formSectionCSS(style.Theme{})
	if !strings.Contains(css, ".fui-form-section__heading {") {
		t.Errorf("the sheet has no rule for the legend's class:\n%s", css)
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

func TestStatCardExtraAttrsOnRoot(t *testing.T) {
	h := StatCard(StatCardConfig{Label: "l", Value: "1", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("StatCard root missing data-test:\n%s", root)
	}
}
func TestCalloutExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"titled":   Callout(CalloutConfig{Title: "t", ExtraAttrs: extra}, render.Text("b")),
		"alert":    Callout(CalloutConfig{Variant: StatusDanger, ExtraAttrs: extra}, render.Text("b")),
		"untitled": Callout(CalloutConfig{ExtraAttrs: extra}, render.Text("b")),
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
		"aria-label": "evil",
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
	if !strings.Contains(string(h), "Save") {
		t.Errorf("label lost:\n%s", h)
	}
}

// Disabled renders the real disabled state; a disabled key in
// ExtraAttrs is the mistake the field exists to make impossible, so
// it panics naming the field rather than silently racing the state.
func TestButtonDisabled(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "Save", Disabled: true}))
	if !strings.Contains(h, "disabled") {
		t.Errorf("Disabled must render the disabled attribute:\n%s", h)
	}
	plain := string(Button(ButtonConfig{Label: "Save"}))
	if strings.Contains(plain, "disabled") {
		t.Errorf("Disabled must be absent when unset:\n%s", plain)
	}
	for _, k := range []string{"disabled", "DISABLED"} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("ExtraAttrs carrying %q must panic", k)
				} else if msg, ok := r.(string); !ok || !strings.Contains(msg, "Disabled") {
					t.Errorf("the panic for %q must point at the field: %v", k, r)
				}
			}()
			Button(ButtonConfig{Label: "Save", ExtraAttrs: html.Attrs{k: ""}})
		}()
	}
}

// The runtime-wiring keys the framework emits ride the typed Action
// seam; everything else a caller passes is still decoration.
func TestButtonRoutesWiringThroughTheActionSeam(t *testing.T) {
	h := string(Button(ButtonConfig{Label: "Edit", ExtraAttrs: html.Attrs{
		"data-fui-open":              "user-edit",
		"data-fui-deeplink":          "user_id=42",
		"data-fui-prefetch":          "menu",
		"data-fui-signal-inc":        "count:1",
		"data-hui-pane-open-control": "secondary",
		"data-fui-confirm":           "Sure?",
		"data-site-ping":             "1",
		"aria-pressed":               "false",
		"data-fui-rpc":               "/__site/x",
		"data-fui-rpc-method":        "POST",
		"data-fui-rpc-signal":        "xsig",
		"data-fui-push-state":        "/after",
		"data-fui-toast":             `{"variant":"info","title":"Hi"}`,
		"data-hui-pane-close":        "",
		"data-fui-rpc-close":         "true",
		"data-fui-rpc-body":          `{"a":1}`,
		"data-fui-rpc-navigate":      "/next",
	}}))
	for _, want := range []string{
		`data-fui-open="user-edit"`, `data-fui-deeplink="user_id=42"`,
		`data-fui-prefetch="menu"`, `data-fui-signal-inc="count:1"`,
		`data-hui-pane-open-control="secondary"`, `data-fui-confirm="Sure?"`,
		`data-site-ping="1"`, `aria-pressed="false"`,
		`data-fui-rpc="/__site/x"`, `data-fui-rpc-method="POST"`,
		`data-fui-rpc-signal="xsig"`, `data-fui-push-state="/after"`,
		`data-fui-toast="{&quot;variant&quot;`, `data-hui-pane-close=""`,
		`data-fui-rpc-close="true"`, `data-fui-rpc-body="{&quot;a&quot;`, `data-fui-rpc-navigate="/next"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in:\n%s", want, h)
		}
	}
}

// A data-fui-* key outside the wiring vocabulary used to render as a
// dead attribute under the old carrier contract; now it panics naming
// the key and the seam it should have used.
func TestButtonPanicsOnAWiringKeyOutsideTheVocabulary(t *testing.T) {
	for _, k := range []string{"data-fui-comp", "data-fui-optimistic-endpoint", "data-fui-toggle-group", "data-fui-anything-else"} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%q outside the vocabulary must panic, not render dead", k)
				} else if msg, ok := r.(string); !ok || !strings.Contains(msg, k) {
					t.Errorf("the panic must name the key %q: %v", k, r)
				}
			}()
			Button(ButtonConfig{Label: "x", ExtraAttrs: html.Attrs{k: "y"}})
		}()
	}
}

// A link carries exactly the four data-fui-* keys that make sense on
// an anchor; the rest are refused as they always were, because a link
// navigates and a button acts.
func TestLinkButtonWiringVocabulary(t *testing.T) {
	h := string(LinkButton(LinkButtonConfig{Label: "Docs", Href: "/docs", ExtraAttrs: html.Attrs{
		"data-fui-push-state": "/docs", "data-fui-prefetch": "menu",
		"data-fui-open": "help", "data-fui-deeplink": "topic=ssh",
		"data-fui-rpc": "/x", "data-fui-signal-inc": "count", "data-fui-toast": `{"a":1}`,
	}}))
	for _, want := range []string{
		`data-fui-push-state="/docs"`, `data-fui-prefetch="menu"`,
		`data-fui-open="help"`, `data-fui-deeplink="topic=ssh"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("a link-legal wiring key was refused:\n%s", h)
		}
	}
	for _, banned := range []string{"data-fui-rpc", "data-fui-signal-inc", "data-fui-toast"} {
		if strings.Contains(h, banned) {
			t.Errorf("%s rode an anchor — a link navigates, a button acts:\n%s", banned, h)
		}
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

// One attribute, one spelling: a key given twice under different
// casings is refused rather than resolved by map order.
func TestButtonExtraAttrsRefuseTwoSpellings(t *testing.T) {
	for _, attrs := range []html.Attrs{
		{"data-fui-open": "a", "DATA-FUI-OPEN": "b"},
		{"data-test": "a", "Data-Test": "b"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ExtraAttrs %v rendered instead of panicking on two spellings", attrs)
				}
			}()
			Button(ButtonConfig{Label: "x", ExtraAttrs: attrs})
		}()
	}
}

// Two headed sections with one heading and a description would share
// the derived description id; an ID roots the second's ids instead.
func TestFormSectionIDRootsTheDescriptionID(t *testing.T) {
	a := string(FormSection(FormSectionConfig{Heading: "Access", Description: "Who may sign in."}))
	b := string(FormSection(FormSectionConfig{Heading: "Access", Description: "Who may sign in.", ID: "access-2"}))
	if !strings.Contains(a, `id="fieldset-access-desc"`) || !strings.Contains(a, `aria-describedby="fieldset-access-desc"`) {
		t.Errorf("the derived description id is not wired:\n%s", a)
	}
	if !strings.Contains(b, `id="access-2-desc"`) || !strings.Contains(b, `aria-describedby="access-2-desc"`) || strings.Contains(b, "fieldset-access-desc") {
		t.Errorf("an explicit ID did not root the description id:\n%s", b)
	}
}

// cfg.Class lands on the empty state's root beside the class map's
// own class, the way every adapter in this file routes it.
func TestEmptyStateAppliesClassOnTheRoot(t *testing.T) {
	h := string(EmptyState(EmptyStateConfig{Title: "No apps", Class: "hero"}))
	if !strings.Contains(h, `class="fui-empty-state hero"`) {
		t.Errorf("the caller's Class did not land after the base class:\n%s", h)
	}
}
