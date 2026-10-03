package ui

// ─── Container ──────────────────────────────────────────────────────
//
// Max-width page wrapper with breakpoint-aware horizontal padding.
// Pairs with Stack/Cluster/Grid (which manage internal spacing).
// Container manages the OUTER bounds: the gutter against the viewport.
// headless.Container renders the measure; this adapter dresses it with
// the fui-container class map.

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ContainerWidth picks the max-inline-size cap.
type ContainerWidth string

const (
	// ContainerNarrow caps at ~640px: long-form prose, marketing.
	ContainerNarrow ContainerWidth = "narrow"
	// ContainerDefault caps at ~1080px: most pages.
	ContainerDefault ContainerWidth = ""
	// ContainerWide caps at the wide width (1280px default): dashboards.
	ContainerWide ContainerWidth = "wide"
	// ContainerPage caps at the page measure (Theme.Layout.PageWidth,
	// --size-page-width, 66rem default): the editorial column of a
	// marketing page.
	// A recipe's main and its header/footer bands share it. Like the
	// bands, the content box is the measure and the page gutter sits
	// outside it, so main's text starts on the header brand's edge.
	ContainerPage ContainerWidth = "page"
	// ContainerFull removes the cap; padding still applies.
	ContainerFull ContainerWidth = "full"
)

// ContainerPad picks the block padding: the page's rhythm between the
// site header, the content and the footer. The two sizes are CSS
// variables (--ui-container-pad-start, --ui-container-pad-end) a theme
// can retune.
type ContainerPad string

const (
	// ContainerPadNone adds no block padding: a container nested inside
	// a page, or a page whose first block owns its own top spacing.
	ContainerPadNone ContainerPad = ""
	// ContainerPadPage pads both ends: roomy under the header
	// (clamp(40px, 6vw, 64px)), roomier above the footer
	// (clamp(48px, 7vw, 80px)). The main region of a marketing or
	// editorial page.
	ContainerPadPage ContainerPad = "page"
	// ContainerPadEnd pads only the end: a compact page whose first
	// block sits right under the header but still clears the footer.
	ContainerPadEnd ContainerPad = "end"
)

// ContainerConfig configures a Container.
type ContainerConfig struct {
	// Width picks the max-inline-size. Defaults to ContainerDefault.
	Width ContainerWidth
	// Pad picks the block padding. Defaults to ContainerPadNone.
	Pad ContainerPad
	// As lets the caller pick a non-<div> tag (e.g. "section", "main").
	// Defaults to "div".
	As    string
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the wrapper element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style and data-fui-*.
	ExtraAttrs html.Attrs
}

// containerClasses dresses headless.Container: the measure names map
// to this package's width vocabulary (narrow / wide / full), the
// default carrying no modifier.
var containerClasses = headless.Classes{
	headless.PartRoot:                "fui-container",
	headless.Part("root--size-sm"):   "fui-container--narrow",
	headless.Part("root--size-lg"):   "fui-container--wide",
	headless.Part("root--size-page"): "fui-container--page",
	headless.Part("root--size-full"): "fui-container--full",
}

// Container renders a max-width wrapper.
func Container(cfg ContainerConfig, children ...render.HTML) render.HTML {
	size := ""
	switch cfg.Width {
	case ContainerNarrow:
		size = "sm"
	case ContainerDefault:
	case ContainerPage:
		size = "page"
	case ContainerWide:
		size = "lg"
	case ContainerFull:
		size = "full"
	default:
		panic("ui: Container unknown Width " + string(cfg.Width) +
			`. Pick one of: narrow, "" (default), page, wide, full`)
	}
	class := cfg.Class
	switch cfg.Pad {
	case ContainerPadNone:
	case ContainerPadPage, ContainerPadEnd:
		class = strings.TrimSpace("fui-container--pad-" + string(cfg.Pad) + " " + class)
	default:
		panic("ui: Container unknown Pad " + string(cfg.Pad) +
			`. Pick one of: "" (none), page, end`)
	}
	return containerStyle.WrapHTML(headless.Container(headless.ContainerProps{
		Size:       size,
		Tag:        cfg.As,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(class),
	}, containerClasses, children...))
}

var containerStyle = registry.RegisterStyle("ui-container", containerCSS)

func containerCSS(_ style.Theme) string {
	// Width caps are theme tokens (Theme.Layout: NarrowWidth,
	// ContentWidth, WideWidth, PageWidth, PageGutter), so a marketing
	// site that wants a 1240px wide cap sets
	//   t.Layout.WideWidth.Value = "1240px"
	// on its theme.
	return `[data-fui-comp="ui-container"] {
  display: block;
  inline-size: 100%;
  max-inline-size: var(--size-content-width, 1080px);
  margin-inline: auto;
  padding-inline: var(--spacing-md, 8px);
  box-sizing: border-box;
}
@media (min-width: 720px) {
  [data-fui-comp="ui-container"] {
    padding-inline: var(--spacing-lg, 16px);
  }
}
@media (min-width: 1080px) {
  [data-fui-comp="ui-container"] {
    padding-inline: var(--spacing-xl, 24px);
  }
}

:where([data-fui-comp="ui-container"]).fui-container--narrow { max-inline-size: var(--size-narrow-width, 640px); }
:where([data-fui-comp="ui-container"]).fui-container--wide { max-inline-size: var(--size-wide-width, 1280px); }
/* The page measure follows the header/footer band model: the CONTENT
   box is the measure and the gutter sits outside it, so main's text
   starts on the same edge as the band's brand at every width. */
:where([data-fui-comp="ui-container"]).fui-container--page {
  --ui-layout-gutter: var(--size-page-gutter, clamp(20px, 5vw, 32px));
  max-inline-size: calc(var(--size-page-width, 66rem) + 2 * var(--ui-layout-gutter));
  padding-inline: var(--ui-layout-gutter);
}
:where([data-fui-comp="ui-container"]).fui-container--full { max-inline-size: none; }
[data-fui-comp="ui-container"].fui-container--pad-page { padding-block: var(--ui-container-pad-start, clamp(40px, 6vw, 64px)) var(--ui-container-pad-end, clamp(48px, 7vw, 80px)); }
[data-fui-comp="ui-container"].fui-container--pad-end { padding-block-end: var(--ui-container-pad-end, clamp(48px, 7vw, 80px)); }`
}
