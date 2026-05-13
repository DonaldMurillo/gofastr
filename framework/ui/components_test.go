package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
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
	})
	mustContain(t, h, "is-error")
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Required field")
	if strings.Contains(string(h), "ui-form-field__help") {
		t.Fatal("expected help to be hidden when Error set")
	}
}

func TestFormFieldHelpRendersWhenNoError(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{Label: "x", For: "n", Help: "Hint", Input: in})
	mustContain(t, h, "Hint")
	mustContain(t, h, "ui-form-field__help")
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

func TestDangerButtonAliasMatchesButtonDanger(t *testing.T) {
	a := string(DangerButton(DangerButtonConfig{Label: "Delete"}))
	b := string(Button(ButtonConfig{Label: "Delete", Variant: ButtonDanger}))
	if a != b {
		t.Errorf("DangerButton alias should match Button{Variant: ButtonDanger}\n--- DangerButton ---\n%s\n--- Button ---\n%s", a, b)
	}
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

// Pre-existing test kept for back-compat coverage of the alias.
func TestDangerButtonHasDangerVariantClass(t *testing.T) {
	h := DangerButton(DangerButtonConfig{Label: "Delete"})
	mustContain(t, h, "ui-button--danger")
	mustContain(t, h, "Delete")
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

// TestStatusBadgeRejectsUnknownVariant mirrors Button — a typo like
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

// ─── Callout ───
// TestCalloutRejectsUnknownVariant mirrors Button/StatusBadge — typo
// must panic instead of silently emitting an unmatched class.
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
