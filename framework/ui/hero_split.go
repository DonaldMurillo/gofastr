package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HeroSplitRatio picks the column ratio.
type HeroSplitRatio string

const (
	// HeroSplitEqual is 1:1, balanced two-column hero.
	HeroSplitEqual HeroSplitRatio = ""
	// HeroSplitCopyWide gives the copy column more room (1.4:1), for a stat band or icon grid.
	HeroSplitCopyWide HeroSplitRatio = "copy"
	// HeroSplitMediaWide gives the media column more room (1:1.2), for code, a screenshot or video.
	HeroSplitMediaWide HeroSplitRatio = "media"
)

// HeroSplitConfig configures a HeroSplit; Copy and Media are both slots.
type HeroSplitConfig struct {
	// Copy is the left column body (title, lede, CTAs).
	Copy render.HTML
	// Media is the right column body (code, image, stats).
	Media render.HTML
	// Ratio picks the column split. Defaults to HeroSplitEqual.
	Ratio HeroSplitRatio
	// AriaLabel names the section. Set it or HeadingID: an unnamed section is no landmark.
	AriaLabel string
	// HeadingID is the id of the heading in Copy that names the section; AriaLabel wins.
	HeadingID string
	// Class is appended to the ui-hero-split wrapper.
	Class string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// HeroSplit renders a two-column hero section.
func HeroSplit(cfg HeroSplitConfig) render.HTML {
	switch cfg.Ratio {
	case HeroSplitEqual, HeroSplitCopyWide, HeroSplitMediaWide:
	default:
		panic("ui: HeroSplit unknown Ratio " + string(cfg.Ratio) +
			`. Pick one of: "" (equal), "copy", "media"`)
	}
	cls := "fui-hero-split"
	if cfg.Ratio != HeroSplitEqual {
		cls += " fui-hero-split--" + string(cfg.Ratio)
	}
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}

	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-label", "aria-labelledby")
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["class"] = cls
	if cfg.AriaLabel != "" {
		attrs["aria-label"] = cfg.AriaLabel
	} else if cfg.HeadingID != "" {
		attrs["aria-labelledby"] = cfg.HeadingID
	}
	var copyOwn, mediaOwn html.Attrs
	if cfg.Copy == "" {
		copyOwn = html.Attrs{"data-cui-internal": ""}
	}
	if cfg.Media == "" {
		mediaOwn = html.Attrs{"data-cui-internal": ""}
	}
	return heroSplitStyle.WrapHTML(render.Tag("section", attrs,
		html.Div(html.DivConfig{Class: "fui-hero-split__copy", ExtraAttrs: copyOwn}, cfg.Copy),
		html.Div(html.DivConfig{Class: "fui-hero-split__media", ExtraAttrs: mediaOwn}, cfg.Media),
	))
}

var heroSplitStyle = registry.RegisterStyle("ui-hero-split", heroSplitCSS)

func heroSplitCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-hero-split"] {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--spacing-2xl, 32px);
  align-items: start;
}
[data-cui-comp="ui-hero-split"].fui-hero-split--copy {
  grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
}
[data-cui-comp="ui-hero-split"].fui-hero-split--media {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
}
[data-cui-comp="ui-hero-split"] .fui-hero-split__copy,
[data-cui-comp="ui-hero-split"] .fui-hero-split__media {
  min-inline-size: 0;
}
@media (max-width: 980px) {
  [data-cui-comp="ui-hero-split"],
  [data-cui-comp="ui-hero-split"].fui-hero-split--copy,
  [data-cui-comp="ui-hero-split"].fui-hero-split--media {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--spacing-lg, 16px);
  }
}`
}
