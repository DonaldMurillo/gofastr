package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestCardRendersHeadingAndBody(t *testing.T) {
	h := Card(CardConfig{Heading: "Recent activity"}, render.Text("BODY"))
	for _, want := range []string{
		`data-cui-comp="ui-card"`,
		"fui-card__heading",
		"Recent activity",
		"BODY",
	} {
		mustContain(t, h, want)
	}
}

func TestCardVariantsEmitClasses(t *testing.T) {
	cases := map[CardVariant]string{
		CardOutlined: "fui-card--outlined",
		CardFlat:     "fui-card--flat",
	}
	for v, want := range cases {
		h := Card(CardConfig{Variant: v, Heading: "x"})
		mustContain(t, h, want)
	}
}

func TestCardElevatedHasNoModifier(t *testing.T) {
	h := Card(CardConfig{Heading: "x"})
	if strings.Contains(string(h), "fui-card--outlined") || strings.Contains(string(h), "fui-card--flat") {
		t.Fatalf("default elevated variant must emit no modifier:\n%s", h)
	}
}

func TestCardInteractiveBecomesAnchor(t *testing.T) {
	h := Card(CardConfig{Heading: "Click me", Href: "/x"})
	mustContain(t, h, `href="/x"`)
	mustContain(t, h, "fui-card--interactive")
}

func TestCardCustomHeaderReplacesAutoBlock(t *testing.T) {
	h := Card(CardConfig{Header: render.Text("CUSTOM_HEADER")}, render.Text("body"))
	mustContain(t, h, "CUSTOM_HEADER")
	if strings.Contains(string(h), "fui-card__heading") {
		t.Fatalf("custom Header should suppress auto-rendered heading:\n%s", h)
	}
}

func TestCardFooterRendersWhenSet(t *testing.T) {
	h := Card(CardConfig{Heading: "x", Footer: render.Text("FOOTER")}, render.Text("body"))
	mustContain(t, h, "fui-card__footer")
	mustContain(t, h, "FOOTER")
}

func TestCardWithoutHeadingFallsBackToDiv(t *testing.T) {
	h := Card(CardConfig{}, render.Text("body"))
	if strings.Contains(string(h), "aria-labelledby") {
		t.Fatalf("no Heading must not emit aria-labelledby:\n%s", h)
	}
	if !strings.Contains(string(h), `data-cui-comp="ui-card"`) {
		t.Fatalf("expected ui-card marker:\n%s", h)
	}
}

func TestCardExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"div":     Card(CardConfig{ExtraAttrs: extra}),
		"section": Card(CardConfig{Heading: "x", ExtraAttrs: extra}),
		"anchor":  Card(CardConfig{Href: "/a", ExtraAttrs: extra}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

func TestCardExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := Card(CardConfig{Href: "/real", Class: "mine", ExtraAttrs: map[string]string{
		"Class": "evil", "href": "javascript:alert(1)", "data-cui-comp": "spoof",
	}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	for _, banned := range []string{"evil", "javascript:", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	mustContain(t, h, `href="/real"`)
}

// HeadingContent puts composed markup inside the card's own heading
// element, so a heading filled in the browser keeps the heading's
// class and level instead of arriving through a filled header.
func TestCardHeadingContentRendersInTheHeadingElement(t *testing.T) {
	h := string(Card(CardConfig{HeadingLevel: 2, HeadingContent: render.HTML(`<span data-slot="x"></span>`)}, render.Text("BODY")))
	want := `<h2 class="fui-card__heading"><span data-slot="x"></span></h2>`
	if !strings.Contains(h, want) {
		t.Fatalf("card = %s\nwant it to contain %s", h, want)
	}
	both := string(Card(CardConfig{Heading: "x", HeadingContent: render.HTML("<b>y</b>")}))
	if !strings.Contains(both, `<h3 class="fui-card__heading"><b>y</b></h3>`) || strings.Contains(both, ">x<") {
		t.Fatalf("HeadingContent must win over Heading: %s", both)
	}
}

// A link card's inner wrapper is the component's own only when nothing
// inside it came from the caller; HeadingContent is caller markup, so
// the wrapper must not be marked internal (an owned sheet's @scope
// stops at an internal mark).
func TestCardLinkWithHeadingContentIsNotInternal(t *testing.T) {
	h := string(Card(CardConfig{Href: "/x", HeadingContent: render.HTML(`<span class="mine">x</span>`)}))
	if strings.Contains(h, "data-cui-internal") {
		t.Fatalf("the inner wrapper of a card holding caller heading content is marked internal: %s", h)
	}
	if plain := string(Card(CardConfig{Href: "/x", Heading: "x"})); !strings.Contains(plain, "data-cui-internal") {
		t.Fatalf("a link card with only its own markup lost its internal mark: %s", plain)
	}
}
