package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestFactBoxLabelFirstDefaultsToLabelThenValueInSource(t *testing.T) {
	h := string(FactBox(FactBoxConfig{Label: "Prereqs", Value: "Go 1.26+, git"}))
	for _, want := range []string{
		`data-fui-comp="ui-fact-box"`,
		`class="ui-fact-box__label"`,
		`>Prereqs<`,
		`class="ui-fact-box__value"`,
		`Go 1.26+, git`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("FactBox missing %q\n%s", want, h)
		}
	}
	labelIdx := strings.Index(h, "ui-fact-box__label")
	valueIdx := strings.Index(h, "ui-fact-box__value")
	if labelIdx == -1 || valueIdx == -1 || labelIdx > valueIdx {
		t.Errorf("LabelFirst (default) Style must put label before value in source order:\n%s", h)
	}
	if strings.Contains(h, "ui-fact-box--value-first") {
		t.Errorf("default Style should not emit --value-first modifier:\n%s", h)
	}
}

func TestFactBoxValueFirstReversesSourceOrderAndAddsModifier(t *testing.T) {
	h := string(FactBox(FactBoxConfig{
		Label: "docs", Value: "53", Style: FactStyleValueFirst,
	}))
	if !strings.Contains(h, "ui-fact-box--value-first") {
		t.Errorf("ValueFirst should emit --value-first modifier class:\n%s", h)
	}
	labelIdx := strings.Index(h, "ui-fact-box__label")
	valueIdx := strings.Index(h, "ui-fact-box__value")
	if labelIdx == -1 || valueIdx == -1 || valueIdx > labelIdx {
		t.Errorf("ValueFirst Style must put value BEFORE label in source order:\n%s", h)
	}
}

func TestFactBoxFullWidthFlag(t *testing.T) {
	h := string(FactBox(FactBoxConfig{Label: "x", Value: "y", FullWidth: true}))
	if !strings.Contains(h, "ui-fact-box--full") {
		t.Errorf("FullWidth should emit modifier class:\n%s", h)
	}
}

func TestFactBoxValueHTMLOverridesValue(t *testing.T) {
	h := string(FactBox(FactBoxConfig{
		Label:     "Or",
		ValueHTML: render.Raw(`<code>kiln serve</code>`),
	}))
	if !strings.Contains(h, "<code>kiln serve</code>") {
		t.Errorf("ValueHTML should render inline:\n%s", h)
	}
}

func TestFactBoxRequiresLabel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("FactBox with empty Label should panic")
		}
	}()
	FactBox(FactBoxConfig{Value: "x"})
}

func TestFactBoxRequiresValueOrValueHTML(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("FactBox with empty Value AND empty ValueHTML should panic")
		}
	}()
	FactBox(FactBoxConfig{Label: "x"})
}

func TestFactBoxRejectsUnknownStyle(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("FactBox with unknown Style should panic")
		}
	}()
	FactBox(FactBoxConfig{Label: "x", Value: "y", Style: "huge"})
}
