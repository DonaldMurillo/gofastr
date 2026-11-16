package ui

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
	// ContainerPage caps at the page measure (--size-page-width, 66rem), the column a
	// recipe's main shares with its header and footer bands; the gutter sits outside it.
	ContainerPage ContainerWidth = "page"
	// ContainerFull removes the cap; padding still applies.
	ContainerFull ContainerWidth = "full"
)

// ContainerPad picks the block padding between the site header, the content and
// the footer. A padded container also takes the page gutter, except in an app frame.
type ContainerPad string

const (
	// ContainerPadNone adds no block padding.
	ContainerPadNone ContainerPad = ""
	// ContainerPadPage pads both ends: the main region of a marketing or editorial page.
	ContainerPadPage ContainerPad = "page"
	// ContainerPadEnd pads only the end, clearing the footer.
	ContainerPadEnd ContainerPad = "end"
)

// ContainerConfig configures a Container.
type ContainerConfig struct {
	// Width picks the max-inline-size. Defaults to ContainerDefault.
	Width ContainerWidth
	// Pad picks the block padding. Defaults to ContainerPadNone.
	Pad ContainerPad
	// Start pins the column to the inline start with no gutter, for a frame that pads
	// its own content. The default centers it.
	Start bool
	// As picks the tag, "div" by default.
	As    string
	ID    string
	Class string
	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

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
	if cfg.Start {
		class = strings.TrimSpace("fui-container--start " + class)
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
	return `[data-cui-comp="ui-container"] {
  display: block;
  inline-size: 100%;
  max-inline-size: var(--size-content-width, 1080px);
  margin-inline: auto;
  padding-inline: var(--spacing-md, 8px);
  box-sizing: border-box;
}
@media (min-width: 720px) {
  [data-cui-comp="ui-container"] {
    padding-inline: var(--spacing-lg, 16px);
  }
}
@media (min-width: 1080px) {
  [data-cui-comp="ui-container"] {
    padding-inline: var(--spacing-xl, 24px);
  }
}

:where([data-cui-comp="ui-container"]).fui-container--narrow { max-inline-size: var(--size-narrow-width, 640px); }
:where([data-cui-comp="ui-container"]).fui-container--wide { max-inline-size: var(--size-wide-width, 1280px); }
/* The page measure follows the header/footer band model: the CONTENT
   box is the measure and the gutter sits outside it, so main's text
   starts on the same edge as the band's brand at every width. */
:where([data-cui-comp="ui-container"]).fui-container--page {
  --ui-layout-gutter: var(--size-page-gutter, clamp(20px, 5vw, 32px));
  max-inline-size: calc(var(--size-page-width, 66rem) + 2 * var(--ui-layout-gutter));
  padding-inline: var(--ui-layout-gutter);
}
:where([data-cui-comp="ui-container"]).fui-container--full { max-inline-size: none; }
/* A page-level container (Pad: Page or End) takes the band gutter at
   every measure, the same gutter the page width uses, so a wide or
   narrow page starts its text on the header and footer bands' edge
   instead of the nested-container steps above. */
:where([data-cui-comp="ui-container"]).fui-container--pad-page,
:where([data-cui-comp="ui-container"]).fui-container--pad-end {
  padding-inline: var(--size-page-gutter, clamp(20px, 5vw, 32px));
}
/* Inside an app frame (ui.ContentRow with a Sidebar) the content area
   pads itself; that padding is the gutter, and a page container there
   adds none, or a phone shows two gutters stacked. The page measure's
   cap drops the gutters it no longer pads. */
:where(.fui-content-row--has-nav main, .fui-content-row--has-nav .layout-content) :where([data-cui-comp="ui-container"]).fui-container--pad-page,
:where(.fui-content-row--has-nav main, .fui-content-row--has-nav .layout-content) :where([data-cui-comp="ui-container"]).fui-container--pad-end {
  --ui-layout-gutter: 0px;
  padding-inline: 0;
}
/* Start: pinned to the inline start inside a frame that owns the
   gutter. Later than the breakpoint padding rules at the same weight,
   so it wins at every width. It pads nothing, so the page measure's
   cap drops the gutters too, the has-nav rule's reasoning. */
:where([data-cui-comp="ui-container"]).fui-container--start { --ui-layout-gutter: 0px; margin-inline: 0 auto; padding-inline: 0; }
[data-cui-comp="ui-container"].fui-container--pad-page { padding-block: var(--ui-container-pad-start, clamp(40px, 6vw, 64px)) var(--ui-container-pad-end, clamp(48px, 7vw, 80px)); }
[data-cui-comp="ui-container"].fui-container--pad-end { padding-block-end: var(--ui-container-pad-end, clamp(48px, 7vw, 80px)); }`
}
