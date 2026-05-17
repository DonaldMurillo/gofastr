package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestCardRendersHeadingAndBody(t *testing.T) {
	h := Card(CardConfig{Heading: "Recent activity"}, render.Text("BODY"))
	for _, want := range []string{
		`data-fui-comp="ui-card"`,
		"ui-card__heading",
		"Recent activity",
		"BODY",
		`aria-labelledby="ui-card-recent-activity"`,
	} {
		mustContain(t, h, want)
	}
}

func TestCardVariantsEmitClasses(t *testing.T) {
	cases := map[CardVariant]string{
		CardOutlined: "ui-card--outlined",
		CardFlat:     "ui-card--flat",
	}
	for v, want := range cases {
		h := Card(CardConfig{Variant: v, Heading: "x"})
		mustContain(t, h, want)
	}
}

func TestCardElevatedHasNoModifier(t *testing.T) {
	h := Card(CardConfig{Heading: "x"})
	if strings.Contains(string(h), "ui-card--outlined") || strings.Contains(string(h), "ui-card--flat") {
		t.Fatalf("default elevated variant must emit no modifier:\n%s", h)
	}
}

func TestCardInteractiveBecomesAnchor(t *testing.T) {
	h := Card(CardConfig{Heading: "Click me", Href: "/x"})
	mustContain(t, h, `href="/x"`)
	mustContain(t, h, "ui-card--interactive")
}

func TestCardCustomHeaderReplacesAutoBlock(t *testing.T) {
	h := Card(CardConfig{Header: render.Text("CUSTOM_HEADER")}, render.Text("body"))
	mustContain(t, h, "CUSTOM_HEADER")
	if strings.Contains(string(h), "ui-card__heading") {
		t.Fatalf("custom Header should suppress auto-rendered heading:\n%s", h)
	}
}

func TestCardFooterRendersWhenSet(t *testing.T) {
	h := Card(CardConfig{Heading: "x", Footer: render.Text("FOOTER")}, render.Text("body"))
	mustContain(t, h, "ui-card__footer")
	mustContain(t, h, "FOOTER")
}

func TestCardWithoutHeadingFallsBackToDiv(t *testing.T) {
	h := Card(CardConfig{}, render.Text("body"))
	if strings.Contains(string(h), "aria-labelledby") {
		t.Fatalf("no Heading must not emit aria-labelledby:\n%s", h)
	}
	if !strings.Contains(string(h), `data-fui-comp="ui-card"`) {
		t.Fatalf("expected ui-card marker:\n%s", h)
	}
}
