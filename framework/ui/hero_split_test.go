package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestHeroSplitRendersBothColumns(t *testing.T) {
	h := string(HeroSplit(HeroSplitConfig{
		Copy:      render.Raw(`<h1>Hello</h1>`),
		Media:     render.Raw(`<pre>code</pre>`),
		AriaLabel: "Hero",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-hero-split"`,
		`aria-label="Hero"`,
		`class="ui-hero-split__copy"`,
		`<h1>Hello</h1>`,
		`class="ui-hero-split__media"`,
		`<pre>code</pre>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("HeroSplit missing %q\n%s", want, h)
		}
	}
}

func TestHeroSplitEqualRatioOmitsModifier(t *testing.T) {
	h := string(HeroSplit(HeroSplitConfig{Copy: render.Raw("a"), Media: render.Raw("b"), AriaLabel: "x"}))
	if strings.Contains(h, "ui-hero-split--") {
		t.Errorf("equal ratio should not emit a modifier class:\n%s", h)
	}
}

func TestHeroSplitCopyWideEmitsModifier(t *testing.T) {
	h := string(HeroSplit(HeroSplitConfig{
		Copy: render.Raw("a"), Media: render.Raw("b"),
		Ratio: HeroSplitCopyWide, AriaLabel: "x",
	}))
	if !strings.Contains(h, "ui-hero-split--copy") {
		t.Errorf("CopyWide should emit --copy modifier:\n%s", h)
	}
}

func TestHeroSplitMediaWideEmitsModifier(t *testing.T) {
	h := string(HeroSplit(HeroSplitConfig{
		Copy: render.Raw("a"), Media: render.Raw("b"),
		Ratio: HeroSplitMediaWide, AriaLabel: "x",
	}))
	if !strings.Contains(h, "ui-hero-split--media") {
		t.Errorf("MediaWide should emit --media modifier:\n%s", h)
	}
}

func TestHeroSplitCSSCollapsesAtMobileBreakpoint(t *testing.T) {
	css := heroSplitCSS(style.Theme{})
	if !strings.Contains(css, "@media (max-width: 980px)") {
		t.Errorf("HeroSplit must collapse to single column on mobile (≤980px):\n%s", css)
	}
	if !strings.Contains(css, "grid-template-columns: 1fr") {
		t.Errorf("HeroSplit mobile branch must set 1fr columns:\n%s", css)
	}
}

func TestHeroSplitGridChildrenSetMinInlineSizeZero(t *testing.T) {
	// Load-bearing: without min-inline-size:0 on grid items, intrinsic
	// content size (long code blocks, wide images) forces the grid
	// past the viewport. See the home hero — heroCodeBlock would
	// blow out the layout without this.
	css := heroSplitCSS(style.Theme{})
	if !strings.Contains(css, "min-inline-size: 0") {
		t.Errorf("HeroSplit grid items must declare min-inline-size:0 "+
			"to allow content shrinkage:\n%s", css)
	}
}

func TestHeroSplitRejectsUnknownRatio(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown Ratio")
		}
	}()
	HeroSplit(HeroSplitConfig{Copy: render.Raw("a"), Media: render.Raw("b"), Ratio: "huge"})
}
