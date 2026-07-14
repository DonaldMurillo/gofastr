package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestStackRendersDataFuiComp(t *testing.T) {
	h := Stack(StackConfig{}, render.Text("A"), render.Text("B"))
	for _, want := range []string{`data-fui-comp="ui-layout"`, "ui-stack", "A", "B"} {
		mustContain(t, h, want)
	}
}

func TestStackGapVariantClass(t *testing.T) {
	h := Stack(StackConfig{Gap: GapLG}, render.Text("x"))
	mustContain(t, h, "ui-layout--gap-lg")
	if strings.Contains(string(h), "ui-layout--gap-md") {
		t.Fatalf("default md gap should not emit a class:\n%s", h)
	}
}

func TestStackAlignJustifyEmitClasses(t *testing.T) {
	h := Stack(StackConfig{Align: AlignCenter, Justify: JustifyBetween}, render.Text("x"))
	mustContain(t, h, "ui-layout--align-center")
	mustContain(t, h, "ui-layout--justify-between")
}

func TestClusterWrapDefaultsOnNoModifierEmitted(t *testing.T) {
	h := Cluster(ClusterConfig{}, render.Text("x"))
	if strings.Contains(string(h), "ui-cluster--nowrap") {
		t.Fatalf("zero-value ClusterConfig must wrap and must NOT emit nowrap modifier:\n%s", h)
	}
}

func TestClusterNoWrapAddsModifier(t *testing.T) {
	h := Cluster(ClusterConfig{NoWrap: true}, render.Text("x"))
	mustContain(t, h, "ui-cluster--nowrap")
}

func TestClusterLegacyWrapTrueStillWraps(t *testing.T) {
	h := Cluster(ClusterConfig{Wrap: true}, render.Text("x"))
	if strings.Contains(string(h), "ui-cluster--nowrap") {
		t.Fatalf("legacy Wrap:true must remain wrapping:\n%s", h)
	}
}

func TestGridRendersMinAsDataAttribute(t *testing.T) {
	h := Grid(GridConfig{Min: "20rem"}, render.Text("x"))
	mustContain(t, h, `data-min="20rem"`)
	mustContain(t, h, "ui-grid")
}

func TestGridDefaultMinFallback(t *testing.T) {
	h := Grid(GridConfig{}, render.Text("x"))
	mustContain(t, h, `data-min="16rem"`)
}

func TestCenterMinHeightVariantClass(t *testing.T) {
	h := Center(CenterConfig{MinHeight: "viewport"}, render.Text("x"))
	mustContain(t, h, "ui-center--viewport")
}

func TestSpacerHasAriaHidden(t *testing.T) {
	h := Spacer()
	mustContain(t, h, `aria-hidden="true"`)
	mustContain(t, h, "ui-spacer")
}

func TestBoxVariantsCompose(t *testing.T) {
	h := Box(BoxConfig{Pad: BoxPadLG, Surface: true, Outlined: true}, render.Text("x"))
	for _, want := range []string{"ui-box", "ui-box--pad-lg", "ui-box--surface", "ui-box--outlined"} {
		mustContain(t, h, want)
	}
}

func TestBoxNoPadEmitsNoPadClass(t *testing.T) {
	h := Box(BoxConfig{}, render.Text("x"))
	if strings.Contains(string(h), "ui-box--pad-") {
		t.Fatalf("BoxPadNone should not emit ui-box--pad-*:\n%s", h)
	}
}

func TestLayoutCustomClassAppended(t *testing.T) {
	h := Stack(StackConfig{Class: "my-extra"}, render.Text("x"))
	mustContain(t, h, "my-extra")
}
