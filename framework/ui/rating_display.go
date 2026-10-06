package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// RatingDisplayConfig configures a read-only Rating.
type RatingDisplayConfig struct {
	// Value is the score shown, 0..Max.
	Value int
	// Max is the number of glyphs. Defaults to 5.
	Max int
	// Label is the accessible name. Defaults to the localized
	// "%d out of %d" (Strings.RatingChoice).
	Label string
	// Shape and Size match RatingInput's, so a review form and the
	// reviews it produces draw the same glyph.
	Shape RatingShape
	Size  RatingSize
	// Ctx resolves the localized default label. Nil means English.
	Ctx        context.Context
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

var ratingDisplayStyle = registry.RegisterStyle("ui-rating-display", ratingDisplayCSS)

// Rating renders a score as a row of glyphs: a testimonial's five
// stars, a product card's average. It is one image with an accessible
// name, not a form control; use RatingInput to collect a score.
func Rating(cfg RatingDisplayConfig) render.HTML {
	max := cfg.Max
	if max == 0 {
		max = 5
	}
	if max < 1 {
		panic("ui: Rating Max must be at least 1")
	}
	if cfg.Value < 0 || cfg.Value > max {
		panic(fmt.Sprintf("ui: Rating Value %d is outside 0..%d", cfg.Value, max))
	}
	switch cfg.Shape {
	case RatingShapeStar, RatingShapeHeart, RatingShapeThumb,
		RatingShapeFire, RatingShapeDiamond, RatingShapeCircle,
		RatingShapeSquare:
	default:
		panic("ui: Rating unknown Shape " + string(cfg.Shape) +
			`. Pick one of: "" (star), heart, thumb, fire, diamond, circle, square`)
	}
	switch cfg.Size {
	case RatingSizeDefault, RatingSizeSmall, RatingSizeLarge:
	default:
		panic("ui: Rating unknown Size " + string(cfg.Size) +
			`. Pick one of: "" (default), small, large`)
	}

	label := cfg.Label
	if label == "" {
		label = fmt.Sprintf(StringsFor(cfg.Ctx).RatingChoice, cfg.Value, max)
	}
	// The shape and size modifiers are RatingInput's own, so one
	// --ui-rating-color override recolours both.
	cls := []string{"fui-rating-display"}
	if cfg.Shape != RatingShapeStar {
		cls = append(cls, "fui-rating--"+string(cfg.Shape))
	}
	if cfg.Size != RatingSizeDefault {
		cls = append(cls, "fui-rating-display--"+string(cfg.Size))
	}
	if cfg.Class != "" {
		cls = append(cls, cfg.Class)
	}

	glyph := render.Raw(ratingIcon(cfg.Shape))
	glyphs := make([]render.HTML, 0, max)
	for i := 1; i <= max; i++ {
		c := "fui-rating-display__glyph"
		if i <= cfg.Value {
			c += " is-on"
		}
		glyphs = append(glyphs, html.Span(html.TextConfig{Class: c, ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, glyph))
	}

	attrs := headless.Safe(cfg.ExtraAttrs, "role", "aria-label", "data-value")
	attrs["role"] = "img"
	attrs["aria-label"] = label
	attrs["data-value"] = strconv.Itoa(cfg.Value)

	return ratingDisplayStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      strings.Join(cls, " "),
		ID:         cfg.ID,
		ExtraAttrs: attrs,
	}, glyphs...))
}

func ratingDisplayCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-rating-display"] {
  --_rating-display-glyph: var(--ui-rating-glyph, 16px);
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  inline-size: fit-content;
  line-height: 0;
}
[data-cui-comp="ui-rating-display"].fui-rating-display--small { --ui-rating-glyph: 12px; }
[data-cui-comp="ui-rating-display"].fui-rating-display--large { --ui-rating-glyph: 24px; }
[data-cui-comp="ui-rating-display"].fui-rating--heart,
[data-cui-comp="ui-rating-display"].fui-rating--fire { --_rating-shape-color: var(--color-danger, #DC2626); }
[data-cui-comp="ui-rating-display"].fui-rating--thumb { --_rating-shape-color: var(--color-primary, #18181B); }
[data-cui-comp="ui-rating-display"].fui-rating--diamond { --_rating-shape-color: var(--color-info, #3B82F6); }
[data-cui-comp="ui-rating-display"] .fui-rating-display__glyph {
  display: inline-flex;
  color: var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-rating-display"] .fui-rating-display__glyph.is-on {
  /* Amber-600 unset, not --color-warning: the warning token is tuned
     dark for text on a chip and paints a star brown. The default lives
     in the fallback, not on the root, so a page or theme that sets
     --ui-rating-color on an ancestor reaches every rating under it; a
     shape's own colour is the default beneath it. */
  color: var(--ui-rating-color, var(--_rating-shape-color, #D97706));
}
[data-cui-comp="ui-rating-display"] .fui-rating-display__glyph svg {
  inline-size: var(--_rating-display-glyph);
  block-size: var(--_rating-display-glyph);
}`
}
