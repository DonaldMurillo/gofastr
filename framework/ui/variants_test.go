package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// Custom variants register at package init, before any component sheet
// is built, exactly as a real app does. These vars stand in for a host
// app's `var Brand = ui.RegisterButtonVariant(...)` declarations.
var (
	testBrandVariant = RegisterButtonVariant("brand", VariantCSS{
		Props: []string{
			"background", "{colors.primary}",
			"color", "#fff",
			"border-color", "transparent",
		},
		Hover: []string{"filter", "none", "opacity", "0.9"},
		Focus: []string{"outline", "2px solid {colors.primary}", "outline-offset", "2px"},
	})
	testHeroSize = RegisterButtonSize("hero", VariantCSS{
		Props: []string{"padding", "18px 32px", "font-size", "1.15rem"},
	})
	testPromoCard = RegisterCardVariant("promo", VariantCSS{
		Props: []string{"border", "2px solid {colors.primary}"},
		Hover: []string{"box-shadow", "{shadows.md}"},
	})
	testBetaStatus = RegisterStatusVariant("beta", StatusVariantCSS{
		Color: "{colors.primary}",
		Icon:  "β",
	})
	_ = RegisterButtonVariant("duptest", VariantCSS{
		Props: []string{"background", "#000"},
	})
)

// wantPanic fails if fn does NOT panic. (mustPanic lives in the
// external ui_test package; this file needs internal access.)
func wantPanic(t *testing.T, msg string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic: %s", msg)
		}
	}()
	fn()
}

func TestRegisteredButtonVariantRenders(t *testing.T) {
	h := Button(ButtonConfig{Label: "Buy", Variant: testBrandVariant})
	mustContain(t, h, "fui-button--brand")
}

func TestLinkButtonHonorsButtonVariant(t *testing.T) {
	h := LinkButton(LinkButtonConfig{Label: "Docs", Href: "/docs", Variant: testBrandVariant})
	mustContain(t, h, "fui-button--brand")
	mustContain(t, h, `href="/docs"`)
}

func TestRegisteredButtonSizeRenders(t *testing.T) {
	h := Button(ButtonConfig{Label: "Go", Size: testHeroSize})
	mustContain(t, h, "fui-button--hero")
}

func TestButtonVariantCSSInSheet(t *testing.T) {
	css := buttonCSS(style.DefaultTheme())
	for _, want := range []string{
		`.fui-button--brand`,
		"var(--color-primary)",
		".fui-button--brand:hover",
		".fui-button--brand:focus-visible",
		`.fui-button--hero`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("ui-button sheet missing %q", want)
		}
	}
}

func TestCustomVariantStylesToggleAction(t *testing.T) {
	// ToggleAction's root is data-fui-comp="ui-toggle-action" wearing
	// the button's own classes. Registered variant rules are plain
	// class rules emitted once, so they must match under that marker
	// exactly as they do under ui-button's.
	css := buttonCSS(style.DefaultTheme())
	h := string(ToggleAction(ToggleActionConfig{
		Endpoint: "/x", IdleLabel: "A", CommittedLabel: "B",
		Variant: testBrandVariant, Size: testHeroSize,
	}))
	if !strings.Contains(h, `data-fui-comp="ui-toggle-action"`) ||
		!strings.Contains(h, "fui-button--brand") {
		t.Fatalf("ToggleAction markup missing marker/variant class:\n%s", h)
	}
	for _, want := range []string{
		`.fui-button--brand {`,
		`.fui-button--hero {`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("ui-button sheet missing the registered class rule %q (and it must match under ANY marker, toggle-action included)", want)
		}
	}
	// The marker-scoped dual copies are gone: one rule, one spelling.
	if strings.Contains(css, `[data-fui-comp="ui-toggle-action"].fui-button--brand`) {
		t.Error("the toggle-action dual scope still ships; the plain class rule already matches it")
	}
}

func TestButtonSheetServesVariantCSS(t *testing.T) {
	e, ok := registry.Lookup("ui-button")
	if !ok {
		t.Fatal("ui-button style not registered")
	}
	css := e.CSSFor(style.DefaultTheme())
	if !strings.Contains(css, ".fui-button--brand") {
		t.Fatalf("registry-served ui-button sheet missing .fui-button--brand:\n%s", css)
	}
}

// A registration lands in the class map as well as the sheet: the
// variant's class is drawn from the same map the built-ins use, so a
// Button rendered with it wears the class a ToggleAction would wear
// too.
func TestRegisteredVariantLandsInTheClassMap(t *testing.T) {
	if got := buttonClasses.Variant(headless.PartRoot, "brand"); got != "fui-button--brand" {
		t.Errorf("root--brand lookup = %q, want fui-button--brand", got)
	}
	if got := buttonClasses.Variant(headless.PartRoot, "hero"); got != "fui-button--hero" {
		t.Errorf("root--hero lookup = %q, want fui-button--hero", got)
	}
	if got := buttonClasses.Class(headless.PartRoot); got != "fui-button" {
		t.Errorf("root class = %q, want fui-button", got)
	}
	if got := buttonClasses.Class(headless.PartIcon); got != "fui-button__icon" {
		t.Errorf("icon class = %q, want fui-button__icon", got)
	}
}

func TestUnregisteredVariantStillPanics(t *testing.T) {
	wantPanic(t, "Button unknown Variant", func() {
		Button(ButtonConfig{Label: "x", Variant: ButtonVariant("nope")})
	})
	wantPanic(t, "Button unknown Size", func() {
		Button(ButtonConfig{Label: "x", Size: ButtonSize("nope")})
	})
	wantPanic(t, "LinkButton unknown Variant", func() {
		LinkButton(LinkButtonConfig{Label: "x", Href: "/x", Variant: ButtonVariant("nope")})
	})
	wantPanic(t, "StatusBadge unknown Variant", func() {
		StatusBadge(StatusBadgeConfig{Label: "x", Variant: StatusVariant("nope")})
	})
	wantPanic(t, "Callout unknown Variant", func() {
		Callout(CalloutConfig{Title: "x", Variant: StatusVariant("nope")})
	})
	wantPanic(t, "Tag unknown Variant", func() {
		Tag(TagConfig{Label: "x", Variant: StatusVariant("nope")})
	})
	wantPanic(t, "Notification unknown Variant", func() {
		Notification(NotificationConfig{Title: "x", Variant: StatusVariant("nope")})
	})
}

func TestDuplicateVariantPanics(t *testing.T) {
	wantPanic(t, "duplicate variant registration", func() {
		RegisterButtonVariant("duptest", VariantCSS{
			Props: []string{"background", "#111"},
		})
	})
}

func TestReservedVariantNamePanics(t *testing.T) {
	wantPanic(t, "built-in button variant name", func() {
		RegisterButtonVariant("primary", VariantCSS{Props: []string{"background", "#111"}})
	})
	wantPanic(t, "built-in button size name", func() {
		RegisterButtonSize("small", VariantCSS{Props: []string{"padding", "2px"}})
	})
	wantPanic(t, "reserved card class name", func() {
		RegisterCardVariant("interactive", VariantCSS{Props: []string{"border", "0"}})
	})
	wantPanic(t, "built-in status variant name", func() {
		RegisterStatusVariant("success", StatusVariantCSS{Color: "#0f0"})
	})
}

func TestBadVariantShapePanics(t *testing.T) {
	wantPanic(t, "odd Props pair count", func() {
		RegisterButtonVariant("odd-props", VariantCSS{Props: []string{"background"}})
	})
	wantPanic(t, "empty Props", func() {
		RegisterButtonVariant("no-props", VariantCSS{})
	})
	wantPanic(t, "invalid name characters", func() {
		RegisterButtonVariant("Bad Name", VariantCSS{Props: []string{"background", "#111"}})
	})
	wantPanic(t, "empty status Color", func() {
		RegisterStatusVariant("no-color", StatusVariantCSS{})
	})
}

func TestRegisterAfterBuildPanics(t *testing.T) {
	_ = buttonCSS(style.DefaultTheme()) // materializes the sheet → seals
	wantPanic(t, "registration after the sheet was built", func() {
		RegisterButtonVariant("too-late", VariantCSS{
			Props: []string{"background", "#111"},
		})
	})
}

func TestCardUnknownVariantPanics(t *testing.T) {
	wantPanic(t, "Card unknown Variant", func() {
		Card(CardConfig{Variant: CardVariant("nope"), Heading: "x"})
	})
}

func TestSidebarBadVariantPanics(t *testing.T) {
	wantPanic(t, "Sidebar unknown Variant", func() {
		// Sidebar is a component. Force render by calling Render().
		_ = Sidebar(SidebarConfig{Variant: SidebarVariant("nope")}).Render()
	})
}

func TestCardRegisteredVariantRenders(t *testing.T) {
	h := Card(CardConfig{Variant: testPromoCard, Heading: "Deal"}, render.Text("body"))
	mustContain(t, h, "ui-card--promo")
	css := cardCSS(style.DefaultTheme())
	for _, want := range []string{
		`[data-fui-comp="ui-card"].ui-card--promo`,
		".ui-card--promo:hover",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("ui-card sheet missing %q", want)
		}
	}
}

func TestStatusVariantSpansComponents(t *testing.T) {
	mustContain(t,
		StatusBadge(StatusBadgeConfig{Label: "Beta", Variant: testBetaStatus}),
		"ui-badge--beta")
	mustContain(t,
		Callout(CalloutConfig{Title: "Beta", Variant: testBetaStatus}, render.Text("b")),
		"ui-callout--beta")
	mustContain(t,
		Tag(TagConfig{Label: "Beta", Variant: testBetaStatus}),
		"ui-tag--beta")
	mustContain(t,
		Notification(NotificationConfig{Title: "Beta", Variant: testBetaStatus}),
		"ui-notification--beta")
}

func TestStatusVariantCSSInAllSheets(t *testing.T) {
	th := style.DefaultTheme()
	cases := []struct {
		sheet string
		css   string
		want  string
	}{
		{"ui-badge", statusBadgeCSS(th), `[data-fui-comp="ui-badge"].ui-badge--beta`},
		{"ui-badge", statusBadgeCSS(th), "color-mix(in oklab, var(--color-primary) 15%"},
		{"ui-tag", tagCSS(th), `[data-fui-comp="ui-tag"].ui-tag--beta`},
		{"ui-callout", calloutCSS(th), "--ui-callout-accent: var(--color-primary)"},
		{"ui-notification", notificationCSS(th), `[data-fui-comp="ui-notification"].ui-notification--beta`},
	}
	for _, c := range cases {
		if !strings.Contains(c.css, c.want) {
			t.Errorf("%s sheet missing %q", c.sheet, c.want)
		}
	}
}

func TestStatusVariantIconFlowsThrough(t *testing.T) {
	if got := notificationGlyph(testBetaStatus); got != "β" {
		t.Errorf("notificationGlyph(beta) = %q, want β", got)
	}
	if !strings.Contains(calloutCSS(style.DefaultTheme()), `"β"`) {
		t.Error("ui-callout sheet missing the registered icon glyph")
	}
}
