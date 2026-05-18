package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Container ──────────────────────────────────────────────────────
//
// Max-width page wrapper with breakpoint-aware horizontal padding.
// Pairs with Stack/Cluster/Grid (which manage internal spacing) —
// Container manages the OUTER bounds: the gutter against the viewport.

// ContainerWidth picks the max-inline-size cap.
type ContainerWidth string

const (
	// ContainerNarrow caps at ~640px — long-form prose, marketing.
	ContainerNarrow ContainerWidth = "narrow"
	// ContainerDefault caps at ~1080px — most pages.
	ContainerDefault ContainerWidth = ""
	// ContainerWide caps at ~1280px — dashboards.
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
	Attrs html.Attrs
}

// Container renders a max-width wrapper.
func Container(cfg ContainerConfig, children ...render.HTML) render.HTML {
	switch cfg.Width {
	case ContainerNarrow, ContainerDefault, ContainerWide, ContainerFull:
	default:
		panic("ui: Container unknown Width " + string(cfg.Width) +
			` — pick one of: narrow, "" (default), wide, full`)
	}
	tag := cfg.As
	if tag == "" {
		tag = "div"
	}
	cls := "ui-container"
	if cfg.Width != ContainerDefault {
		cls += " ui-container--" + string(cfg.Width)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.Attrs {
		attrs[k] = v
	}
	return containerStyle.WrapHTML(render.Tag(tag, attrs, children...))
}

var containerStyle = registry.RegisterStyle("ui-container", containerCSS)

func containerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-container"] {
  display: block;
  inline-size: 100%;
  max-inline-size: 1080px;
  margin-inline: auto;
  padding-inline: var(--spacing-md, 16px);
  box-sizing: border-box;
}
@media (min-width: 720px) {
  [data-fui-comp="ui-container"] {
    padding-inline: var(--spacing-lg, 24px);
  }
}
@media (min-width: 1080px) {
  [data-fui-comp="ui-container"] {
    padding-inline: var(--spacing-xl, 32px);
  }
}

[data-fui-comp="ui-container"].ui-container--narrow { max-inline-size: 640px; }
[data-fui-comp="ui-container"].ui-container--wide   { max-inline-size: 1280px; }
[data-fui-comp="ui-container"].ui-container--full   { max-inline-size: none; }`
}
