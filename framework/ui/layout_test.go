package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestStackRendersDataFuiComp(t *testing.T) {
	h := Stack(StackConfig{}, render.Text("A"), render.Text("B"))
	for _, want := range []string{`data-fui-comp="ui-layout"`, "fui-stack", "A", "B"} {
		mustContain(t, h, want)
	}
}

func TestStackGapVariantClass(t *testing.T) {
	h := Stack(StackConfig{Gap: GapLG}, render.Text("x"))
	mustContain(t, h, "fui-layout--gap-lg")
	if strings.Contains(string(h), "fui-layout--gap-md") {
		t.Fatalf("default md gap should not emit a class:\n%s", h)
	}
}

func TestStackAlignJustifyEmitClasses(t *testing.T) {
	h := Stack(StackConfig{Align: AlignCenter, Justify: JustifyBetween}, render.Text("x"))
	mustContain(t, h, "fui-layout--align-center")
	mustContain(t, h, "fui-layout--justify-between")
}

func TestClusterWrapDefaultsOnNoModifierEmitted(t *testing.T) {
	h := Cluster(ClusterConfig{}, render.Text("x"))
	if strings.Contains(string(h), "fui-cluster--nowrap") {
		t.Fatalf("zero-value ClusterConfig must wrap and must NOT emit nowrap modifier:\n%s", h)
	}
}

func TestClusterNoWrapAddsModifier(t *testing.T) {
	h := Cluster(ClusterConfig{NoWrap: true}, render.Text("x"))
	mustContain(t, h, "fui-cluster--nowrap")
}

func TestGridRendersMinAsDataAttribute(t *testing.T) {
	h := Grid(GridConfig{Min: "20rem"}, render.Text("x"))
	mustContain(t, h, `data-min="20rem"`)
	mustContain(t, h, "fui-grid")
}

func TestGridDefaultMinFallback(t *testing.T) {
	h := Grid(GridConfig{}, render.Text("x"))
	mustContain(t, h, `data-min="16rem"`)
}

func TestCenterMinHeightVariantClass(t *testing.T) {
	h := Center(CenterConfig{MinHeight: "viewport"}, render.Text("x"))
	mustContain(t, h, "fui-center--viewport")
}

func TestSpacerHasAriaHidden(t *testing.T) {
	h := Spacer()
	mustContain(t, h, `aria-hidden="true"`)
	mustContain(t, h, "fui-spacer")
}

func TestBoxVariantsCompose(t *testing.T) {
	h := Box(BoxConfig{Pad: BoxPadLG, Surface: true, Outlined: true}, render.Text("x"))
	for _, want := range []string{"fui-box", "fui-box--pad-lg", "fui-box--surface", "fui-box--outlined"} {
		mustContain(t, h, want)
	}
}

func TestBoxNoPadEmitsNoPadClass(t *testing.T) {
	h := Box(BoxConfig{}, render.Text("x"))
	if strings.Contains(string(h), "fui-box--pad-") {
		t.Fatalf("BoxPadNone should not emit fui-box--pad-*:\n%s", h)
	}
}

func TestLayoutCustomClassAppended(t *testing.T) {
	h := Stack(StackConfig{Class: "my-extra"}, render.Text("x"))
	mustContain(t, h, "my-extra")
}

// ─── ExtraAttrs pass-through (#251) ───

func TestStackExtraAttrsOnRoot(t *testing.T) {
	h := Stack(StackConfig{ExtraAttrs: map[string]string{"data-test": "hook"}}, render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Stack root missing data-test:\n%s", root)
	}
}

func TestClusterExtraAttrsOnRoot(t *testing.T) {
	h := Cluster(ClusterConfig{ExtraAttrs: map[string]string{"data-test": "hook"}}, render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Cluster root missing data-test:\n%s", root)
	}
}

func TestGridExtraAttrsOnRoot(t *testing.T) {
	h := Grid(GridConfig{ExtraAttrs: map[string]string{"data-test": "hook", "data-min": "evil"}},
		render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Grid root missing data-test:\n%s", root)
	}
	mustContain(t, h, `data-min="16rem"`)
}

func TestCenterExtraAttrsOnRoot(t *testing.T) {
	h := Center(CenterConfig{ExtraAttrs: map[string]string{"data-test": "hook"}}, render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Center root missing data-test:\n%s", root)
	}
}

func TestBoxExtraAttrsOnRoot(t *testing.T) {
	h := Box(BoxConfig{ExtraAttrs: map[string]string{"data-test": "hook"}}, render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Box root missing data-test:\n%s", root)
	}
}

func TestStickyExtraAttrsOnRoot(t *testing.T) {
	h := Sticky(StickyConfig{ExtraAttrs: map[string]string{"data-test": "hook", "data-fui-z-tier": "modal"}},
		render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Sticky root missing data-test:\n%s", root)
	}
	mustContain(t, h, `data-fui-z-tier="sticky"`)
}

func TestAspectRatioExtraAttrsOnRoot(t *testing.T) {
	h := AspectRatioComponent(AspectRatioConfig{
		Ratio: AspectRatio16_9, ExtraAttrs: map[string]string{"data-test": "hook"},
	}, render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("AspectRatio root missing data-test:\n%s", root)
	}
	mustContain(t, h, `data-fui-comp="ui-aspect-ratio"`)
}

// The styled layout adapters own their roots the way the headless
// adapters do: a hostile ExtraAttrs map cannot land style in any
// spelling, forge the comp marker, or plant a runtime hook. The
// doc comment promises exactly this refusal.
func TestStyledLayoutAdaptersRefuseHostileExtraAttrs(t *testing.T) {
	hostile := map[string]string{
		"style":         "color:red",
		"STYLE":         "color:red",
		"data-fui-comp": "spoof",
		"data-hui-x":    "1",
	}
	kids := []render.HTML{render.Text("x")}
	for name, h := range map[string]render.HTML{
		"center": Center(CenterConfig{ExtraAttrs: hostile}, kids...),
		"box":    Box(BoxConfig{ExtraAttrs: hostile}, kids...),
		"sticky": Sticky(StickyConfig{ExtraAttrs: hostile}, kids...),
		"ar":     AspectRatioComponent(AspectRatioConfig{Ratio: AspectRatio1_1, ExtraAttrs: hostile}, kids[0]),
	} {
		s := string(h)
		root := s[:strings.Index(s, ">")+1]
		for _, banned := range []string{`style=`, `STYLE=`, `"spoof"`, `data-hui-x`} {
			if strings.Contains(root, banned) {
				t.Errorf("%s: hostile key %q reached the root:\n%s", name, banned, root)
			}
		}
	}
}

// A caller's Class lands on the root beside the data-min hook: the
// adapter builds the root attrs itself, and the class must ride in
// them rather than in a part map the attrs then replace.
func TestGridAppliesClassOnTheRoot(t *testing.T) {
	h := string(Grid(GridConfig{Class: "catalog", Min: "20rem"}, render.Text("x")))
	if !strings.Contains(h, `class="fui-layout fui-grid catalog"`) {
		t.Errorf("Grid dropped the caller's Class:\n%s", h)
	}
	if !strings.Contains(h, `data-min="20rem"`) {
		t.Errorf("Grid lost its data-min beside the Class:\n%s", h)
	}
}
