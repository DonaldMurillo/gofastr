package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Thumbnail ──────────────────────────────────────────────────────
//
// A small fixed-size image preview: a table cell's photo, a record's
// image field. The image covers a square box with rounded corners, so
// mixed aspect ratios line up in a column.

// ThumbnailSize sets the box's edge.
type ThumbnailSize string

const (
	ThumbnailSM ThumbnailSize = "sm" // 2rem, a table row
	ThumbnailMD ThumbnailSize = ""   // 4rem (default)
	ThumbnailLG ThumbnailSize = "lg" // 8rem, a record page
)

// ThumbnailConfig configures a thumbnail.
type ThumbnailConfig struct {
	// Src is the image URL. A value urlsafe's image policy refuses (a
	// script scheme, an SVG or HTML data URI) draws nothing.
	Src string
	// Alt is the image's accessible name; empty marks it decorative.
	Alt  string
	Size ThumbnailSize

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// ARIA overrides) to the root <span>. Keys the component owns are
	// dropped: class and id (use Class / ID) and data-cui-*.
	ExtraAttrs html.Attrs
}

var thumbnailStyle = registry.RegisterStyle("ui-thumbnail", thumbnailCSS)

// Thumbnail renders a lazily loaded image in a square box. It returns ""
// when Src is empty or refused, so a caller draws its own empty mark.
func Thumbnail(cfg ThumbnailConfig) render.HTML {
	src := urlsafe.Clean(cfg.Src, urlsafe.ImageSource)
	if src == "" {
		return ""
	}
	cls := "fui-thumbnail"
	if cfg.Size != ThumbnailMD {
		cls += " fui-thumbnail--" + string(cfg.Size)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	img := html.Image(html.ImageConfig{
		Src: src, Alt: cfg.Alt, Class: "fui-thumbnail__img",
		ExtraAttrs: html.Attrs{"loading": "lazy", "decoding": "async", "data-cui-internal": ""},
	})
	return thumbnailStyle.WrapHTML(html.Span(html.TextConfig{Class: cls, ID: cfg.ID, ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}, img))
}

func thumbnailCSS(_ style.Theme) string {
	// Knobs: --ui-thumbnail-size (4rem) and its -sm (2rem) and -lg (8rem)
	// variants.
	return `[data-cui-comp="ui-thumbnail"] {
  display: inline-block;
  flex-shrink: 0;
  vertical-align: middle;
  inline-size: var(--ui-thumbnail-size, 4rem);
  block-size: var(--ui-thumbnail-size, 4rem);
  border-radius: var(--radii-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  background: var(--color-surface-soft, #F4F4F5);
  overflow: hidden;
}
:where([data-cui-comp="ui-thumbnail"]).fui-thumbnail--sm { inline-size: var(--ui-thumbnail-size-sm, 2rem); block-size: var(--ui-thumbnail-size-sm, 2rem); border-radius: var(--radii-sm, 6px); }
:where([data-cui-comp="ui-thumbnail"]).fui-thumbnail--lg { inline-size: var(--ui-thumbnail-size-lg, 8rem); block-size: var(--ui-thumbnail-size-lg, 8rem); border-radius: var(--radii-lg, 10px); }
[data-cui-comp="ui-thumbnail"] .fui-thumbnail__img {
  display: block;
  inline-size: 100%;
  block-size: 100%;
  object-fit: cover;
}
`
}
