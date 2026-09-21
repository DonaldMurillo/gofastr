package ui

// ─── Container ──────────────────────────────────────────────────────
//
// Max-width page wrapper with breakpoint-aware horizontal padding.
// Pairs with Stack/Cluster/Grid (which manage internal spacing).
// Container manages the OUTER bounds: the gutter against the viewport.
// headless.Container renders the measure; this adapter dresses it with
// the fui-container class map.

import (
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
	// ContainerWide caps at ~1280px: dashboards.
	ContainerWide ContainerWidth = "wide"
	// ContainerFull removes the cap; padding still applies.
	ContainerFull ContainerWidth = "full"
)

// ContainerConfig configures a Container.
type ContainerConfig struct {
	// Width picks the max-inline-size. Defaults to ContainerDefault.
	Width ContainerWidth
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
	headless.Part("root--size-full"): "fui-container--full",
}

// Container renders a max-width wrapper.
func Container(cfg ContainerConfig, children ...render.HTML) render.HTML {
	size := ""
	switch cfg.Width {
	case ContainerNarrow:
		size = "sm"
	case ContainerDefault:
	case ContainerWide:
		size = "lg"
	case ContainerFull:
		size = "full"
	default:
		panic("ui: Container unknown Width " + string(cfg.Width) +
			`. Pick one of: narrow, "" (default), wide, full`)
	}
	return containerStyle.WrapHTML(headless.Container(headless.ContainerProps{
		Size:       size,
		Tag:        cfg.As,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cfg.Class),
	}, containerClasses, children...))
}

var containerStyle = registry.RegisterStyle("ui-container", containerCSS)

func containerCSS(_ style.Theme) string {
	// Width caps are exposed as CSS variables so a host theme can
	// override them without forking the component. Example: a marketing
	// site that wants a 1240px wide cap sets
	//   :root { --ui-container-wide: 1240px; }
	// in its app.css.
	return `[data-fui-comp="ui-container"] {
  display: block;
  inline-size: 100%;
  max-inline-size: var(--ui-container-default, 1080px);
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

[data-fui-comp="ui-container"].fui-container--narrow { max-inline-size: var(--ui-container-narrow, 640px); }
[data-fui-comp="ui-container"].fui-container--wide   { max-inline-size: var(--ui-container-wide, 1280px); }
[data-fui-comp="ui-container"].fui-container--full   { max-inline-size: none; }`
}
