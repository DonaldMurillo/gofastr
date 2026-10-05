package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// ruleBody returns the declarations of the first rule whose selector
// is exactly sel, or fails the test. A rule that starts its own line
// wins over a selector-list tail ending in sel.
func ruleBody(t *testing.T, css, sel string) string {
	t.Helper()
	i := -1
	if strings.HasPrefix(css, sel+" {") {
		i = 0
	} else if j := strings.Index(css, "\n"+sel+" {"); j >= 0 {
		i = j + 1
	}
	if i < 0 {
		i = strings.Index(css, sel+" {")
	}
	if i < 0 {
		i = strings.Index(css, sel+"{")
	}
	if i < 0 {
		t.Fatalf("rule %q missing from:\n%s", sel, css)
	}
	rule := css[i:]
	return rule[:strings.Index(rule, "}")]
}

// pressChain mirrors the fallback order every clickable surface reads:
// the component's own knob, the shared --ui-press-* knob, then the
// state before it (hover falls back to rest, active to hover).
func pressChain(comp, rest string) (hoverT, activeT, hoverS, activeS string) {
	hoverT = "var(--ui-" + comp + "-hover-translate, var(--ui-press-hover-translate, none))"
	activeT = "var(--ui-" + comp + "-active-translate, var(--ui-press-active-translate, " + hoverT + "))"
	hoverS = "var(--ui-" + comp + "-hover-shadow, var(--ui-press-hover-shadow, " + rest + "))"
	activeS = "var(--ui-" + comp + "-active-shadow, var(--ui-press-active-shadow, " + hoverS + "))"
	return
}

// A press-down theme (neo-brutalism: lift on hover, sink flat on click)
// reaches every clickable surface from the shared knobs alone, and the
// default stays still.
func TestPressKnobsReachClickables(t *testing.T) {
	th := style.DefaultTheme()
	for _, c := range []struct {
		name, css, hover, active, comp, rest, shadowProp string
	}{
		{"button", buttonCSS(th), `.fui-button:hover:not(:disabled, [aria-disabled="true"])`, `.fui-button:active:not(:disabled, [aria-disabled="true"])`, "button", "var(--ui-button-shadow, var(--shadow-xs))", "--fui-button-state-shadow"},
		{"card", cardCSS(th), `[data-cui-comp="ui-card"].fui-card--interactive:hover`, `[data-cui-comp="ui-card"].fui-card--interactive:active`, "card", "var(--shadow-sm)", "box-shadow"},
		{"tag", tagCSS(th), `[data-cui-comp="ui-tag"].fui-tag--interactive:hover`, `[data-cui-comp="ui-tag"].fui-tag--interactive:active`, "tag", "none", "box-shadow"},
		{"gallery", galleryCSS(th), `[data-cui-comp="ui-gallery"] .fui-gallery__item:hover`, `[data-cui-comp="ui-gallery"] .fui-gallery__item:active`, "gallery-item", "none", "box-shadow"},
		{"back-to-top", backToTopCSS(th), `[data-cui-comp="ui-back-to-top"]:hover`, `[data-cui-comp="ui-back-to-top"]:active`, "back-to-top", "var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.1))", "box-shadow"},
		{"theme-toggle", themeToggleCSS(th), `:where(button)[data-cui-comp="ui-theme-toggle"]:hover`, `:where(button)[data-cui-comp="ui-theme-toggle"]:active`, "theme-toggle", "none", "box-shadow"},
		{"copy-btn", copyButtonCSS(th), `[data-cui-comp="ui-copy-btn"] .fui-copy-btn:hover`, `[data-cui-comp="ui-copy-btn"] .fui-copy-btn:active`, "copy-btn", "var(--ui-copy-btn-shadow, var(--shadow-xs))", "box-shadow"},
		{"carousel-arrow", carouselCSS(th), `[data-cui-comp="ui-carousel"] .fui-carousel__next:hover`, `[data-cui-comp="ui-carousel"] .fui-carousel__next:active`, "carousel-arrow", "var(--shadow-xs)", "box-shadow"},
		{"menu-trigger", menuCSS(th), `[data-cui-comp="ui-menu"] > summary.fui-menu__trigger:hover`, `[data-cui-comp="ui-menu"] > summary.fui-menu__trigger:active`, "menu-trigger", "var(--shadow-xs)", "box-shadow"},
	} {
		hoverT, activeT, hoverS, activeS := pressChain(c.comp, c.rest)
		hover := ruleBody(t, c.css, c.hover)
		active := ruleBody(t, c.css, c.active)
		for _, w := range []struct{ rule, want string }{
			{hover, "translate: " + hoverT + ";"},
			{hover, c.shadowProp + ": " + hoverS + ";"},
			{active, "translate: " + activeT + ";"},
			{active, c.shadowProp + ": " + activeS + ";"},
		} {
			if !strings.Contains(w.rule, w.want) {
				t.Errorf("%s: missing %q in:\n%s", c.name, w.want, w.rule)
			}
		}
		// Motion is the translate property, never transform, so a
		// component that positions the element with transform (the
		// back-to-top slide) keeps it under the pointer.
		for _, rule := range []string{hover, active} {
			if strings.Contains(rule, "transform:") {
				t.Errorf("%s: a pointer state must not write transform:\n%s", c.name, rule)
			}
		}
	}
}

// The button's states write a private variable, never box-shadow, so a
// component that sets its button's shadow outright keeps it on hover.
func TestButtonStatesKeepOwnShadow(t *testing.T) {
	css := buttonCSS(style.DefaultTheme())
	for _, sel := range []string{`.fui-button:hover:not(:disabled, [aria-disabled="true"])`, `.fui-button:active:not(:disabled, [aria-disabled="true"])`} {
		if body := ruleBody(t, css, sel); strings.Contains(body, "box-shadow:") {
			t.Errorf("%s must not write box-shadow:\n%s", sel, body)
		}
	}
	for _, v := range []string{"primary", "danger", "secondary"} {
		body := ruleBody(t, css, ".fui-button--"+v)
		want := "box-shadow: var(--fui-button-state-shadow, var(--ui-button-shadow, var(--shadow-xs)));"
		if !strings.Contains(body, want) {
			t.Errorf("%s button must read the state shadow, got:\n%s", v, body)
		}
	}
	base := ruleBody(t, css, ".fui-button")
	for _, want := range []string{"translate var(--duration-fast, 150ms) ease", "box-shadow var(--duration-fast, 150ms) ease"} {
		if !strings.Contains(base, want) {
			t.Errorf("button transition must ease %q, got:\n%s", want, base)
		}
	}
}

// Rows and chrome stay still: a row card is a list line and the framed
// code block's copy button is head chrome, not a tile.
func TestPressSkipsRowsAndChrome(t *testing.T) {
	th := style.DefaultTheme()
	card := cardCSS(th)
	for _, sel := range []string{
		`[data-cui-comp="ui-card"].fui-card--row.fui-card--interactive:hover`,
		`[data-cui-comp="ui-card"].fui-card--row.fui-card--interactive:active`,
	} {
		body := ruleBody(t, card, sel)
		if !strings.Contains(body, "translate: none") || !strings.Contains(body, "box-shadow: none") {
			t.Errorf("%s must pin translate and box-shadow to none, got:\n%s", sel, body)
		}
	}
	code := codeBlockCSS(th)
	for _, knob := range []string{"hover-shadow", "active-shadow", "hover-translate", "active-translate"} {
		if !strings.Contains(code, "--ui-copy-btn-"+knob+": none;") {
			t.Errorf("framed code block head must set --ui-copy-btn-%s: none", knob)
		}
	}
}

// Letter case is a look value too: every kicker, chip and button label
// reads a knob, defaulting to what it draws today.
func TestTextCaseKnobs(t *testing.T) {
	th := style.DefaultTheme()
	for _, c := range []struct{ css, sel, want string }{
		{buttonCSS(th), ".fui-button", "var(--ui-button-case, none)"},
		{statusBadgeCSS(th), `[data-cui-comp="ui-badge"]`, "var(--ui-badge-case, none)"},
		{tagCSS(th), `[data-cui-comp="ui-tag"]`, "var(--ui-tag-case, none)"},
		{sectionCSS(th), `[data-cui-comp="ui-section"] .fui-section__eyebrow`, "var(--ui-section-eyebrow-case, none)"},
		{pageHeaderCSS(th), ".fui-page-header__eyebrow", "var(--ui-page-header-eyebrow-case, none)"},
		{recordSummaryCSS(th), ".fui-record-summary__eyebrow", "var(--ui-record-summary-eyebrow-case, none)"},
		{anchoredRailCSS(th), ".fui-anchored-rail__eyebrow", "var(--ui-anchored-rail-eyebrow-case, none)"},
		{statusPillCSS(th), `[data-cui-comp="ui-status-pill"]`, "var(--ui-status-pill-case, none)"},
		{pricingCardCSS(th), ".fui-pricing-card__badge", "var(--ui-pricing-card-badge-case, none)"},
	} {
		body := ruleBody(t, c.css, c.sel)
		if !strings.Contains(body, "text-transform: "+c.want) && !strings.Contains(body, "text-transform:"+c.want) {
			t.Errorf("%s must set text-transform from %s, got:\n%s", c.sel, c.want, body)
		}
	}
}

// A gap is the room between things, so a theme whose cards cast a hard
// offset shadow widens it without inflating every padding: each layout
// gap step reads its own knob over the spacing token.
func TestLayoutGapKnobs(t *testing.T) {
	css := layoutCSS(style.DefaultTheme())
	md := "gap: var(--ui-layout-gap-md, var(--spacing-md, 8px));"
	for _, sel := range []string{"fui-stack", "fui-cluster", "fui-grid"} {
		if body := ruleBody(t, css, `:where([data-cui-comp="ui-layout"]).`+sel); !strings.Contains(body, md) {
			t.Errorf("%s must read %s, got:\n%s", sel, md, body)
		}
	}
	// A section stacks its body's blocks (a toggle, a card grid, a
	// table) on its own grid, so its gap takes a knob too, over the
	// layout step it draws.
	body := ruleBody(t, sectionCSS(style.DefaultTheme()), `[data-cui-comp="ui-section"] .fui-section__body`)
	if want := "gap: var(--ui-section-body-gap, var(--ui-layout-gap-lg, var(--spacing-lg, 16px)));"; !strings.Contains(body, want) {
		t.Errorf("section body must read %s, got:\n%s", want, body)
	}
	// The carousel's slide width subtracts the track gap, so both read
	// the one knob or a widened gap pushes the last column off the stage.
	car := carouselCSS(style.DefaultTheme())
	cg := "var(--ui-carousel-gap, var(--ui-layout-gap-md, var(--spacing-md, 8px)))"
	if body := ruleBody(t, car, `[data-cui-comp="ui-carousel"] .fui-carousel__track`); !strings.Contains(body, "gap: "+cg+";") {
		t.Errorf("carousel track must read gap: %s, got:\n%s", cg, body)
	}
	if body := ruleBody(t, car, `[data-cui-comp="ui-carousel"] .fui-carousel__slide`); !strings.Contains(body, "* "+cg+")") {
		t.Errorf("carousel slide width must subtract %s, got:\n%s", cg, body)
	}
	// The gallery used to write --ui-gallery-gap on its own root, which
	// shadowed any value a theme set on :root. It resolves a private
	// property from the knob instead, and each Gap preset reads its
	// layout step.
	gal := galleryCSS(style.DefaultTheme())
	if root := ruleBody(t, gal, `[data-cui-comp="ui-gallery"]`); strings.Contains(root, "--ui-gallery-gap:") ||
		!strings.Contains(root, "--_gallery-gap: var(--ui-gallery-gap, var(--ui-layout-gap-md, var(--spacing-md, 8px)));") {
		t.Errorf("gallery root must resolve --_gallery-gap from the knob without setting it, got:\n%s", root)
	}
	for _, c := range []struct{ step, px string }{{"xs", "2px"}, {"sm", "4px"}, {"lg", "16px"}, {"xl", "24px"}} {
		want := "{ --_gallery-gap: var(--ui-layout-gap-" + c.step + ", var(--spacing-" + c.step + ", " + c.px + ")); }"
		if !strings.Contains(gal, ".fui-gallery--gap-"+c.step+" "+want) {
			t.Errorf("gallery gap-%s preset must read %s", c.step, want)
		}
	}
	if strings.Contains(gal, "var(--ui-gallery-gap)") {
		t.Error("gallery rules must read --_gallery-gap, not the bare knob")
	}
	for _, c := range []struct{ step, px string }{{"xs", "2px"}, {"sm", "4px"}, {"lg", "16px"}, {"xl", "24px"}, {"2xl", "32px"}} {
		want := "gap: var(--ui-layout-gap-" + c.step + ", var(--spacing-" + c.step + ", " + c.px + "));"
		line := css[strings.Index(css, ".fui-layout--gap-"+c.step+" "):]
		if line = line[:strings.Index(line, "}")]; !strings.Contains(line, want) {
			t.Errorf("gap-%s must read %s, got %q", c.step, want, line)
		}
	}
}
