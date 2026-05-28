package ui

// HeroSplit — two-column hero section. Copy on one side, media on the
// other. The shape recurs across docs/marketing surfaces: a code block
// next to the pitch, a stat band next to a docs intro, a screenshot
// next to a feature description. The framework owns the grid + the
// mobile single-column collapse; the consumer fills both slots.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HeroSplitRatio picks the column ratio. The three values cover the
// shapes that actually show up — pages that want something else
// should write a one-off and not abuse the enum.
type HeroSplitRatio string

const (
	// HeroSplitEqual is 1:1 — balanced two-column hero.
	HeroSplitEqual HeroSplitRatio = ""
	// HeroSplitCopyWide gives the copy column more room (1.4:1).
	// Use when the right slot is a compact stat band or icon grid.
	HeroSplitCopyWide HeroSplitRatio = "copy"
	// HeroSplitMediaWide gives the media column more room (1:1.2).
	// Use when the right slot is a code block, screenshot, or video.
	HeroSplitMediaWide HeroSplitRatio = "media"
)

// HeroSplitConfig configures a HeroSplit. Copy and Media are both
// slots — the framework does not assume their contents. Pair with
// ui.Container if you want a max-width wrapper around the hero.
type HeroSplitConfig struct {
	// Copy is the left column body (title, lede, CTAs).
	Copy render.HTML
	// Media is the right column body (code, image, stats).
	Media render.HTML
	// Ratio picks the column split. Defaults to HeroSplitEqual.
	Ratio HeroSplitRatio
	// AriaLabel labels the <section> for screen readers (the hero is
	// almost always the page-opening landmark). Required unless an
	// h1 inside Copy provides the accessible name.
	AriaLabel string
	// Class is appended to the ui-hero-split wrapper.
	Class string
}

// HeroSplit renders a two-column hero. The wrapper is a <section>;
// callers pass the content for each column as HTML.
func HeroSplit(cfg HeroSplitConfig) render.HTML {
	switch cfg.Ratio {
	case HeroSplitEqual, HeroSplitCopyWide, HeroSplitMediaWide:
	default:
		panic("ui: HeroSplit unknown Ratio " + string(cfg.Ratio) +
			` — pick one of: "" (equal), "copy", "media"`)
	}
	cls := "ui-hero-split"
	if cfg.Ratio != HeroSplitEqual {
		cls += " ui-hero-split--" + string(cfg.Ratio)
	}
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}

	attrs := map[string]string{"class": cls}
	if cfg.AriaLabel != "" {
		attrs["aria-label"] = cfg.AriaLabel
	}
	return heroSplitStyle.WrapHTML(render.Tag("section", attrs,
		html.Div(html.DivConfig{Class: "ui-hero-split__copy"}, cfg.Copy),
		html.Div(html.DivConfig{Class: "ui-hero-split__media"}, cfg.Media),
	))
}

var heroSplitStyle = registry.RegisterStyle("ui-hero-split", heroSplitCSS)

func heroSplitCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-hero-split"] {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--spacing-2xl, 48px);
  align-items: start;
}
[data-fui-comp="ui-hero-split"].ui-hero-split--copy {
  grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
}
[data-fui-comp="ui-hero-split"].ui-hero-split--media {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
}
[data-fui-comp="ui-hero-split"] .ui-hero-split__copy,
[data-fui-comp="ui-hero-split"] .ui-hero-split__media {
  min-inline-size: 0;
}
@media (max-width: 980px) {
  [data-fui-comp="ui-hero-split"],
  [data-fui-comp="ui-hero-split"].ui-hero-split--copy,
  [data-fui-comp="ui-hero-split"].ui-hero-split--media {
    grid-template-columns: 1fr;
    gap: var(--spacing-lg, 24px);
  }
}`
}
