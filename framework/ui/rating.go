package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// RatingShape picks one of the bundled glyphs. For a custom glyph,
// set RatingConfig.Icon instead. Icon overrides Shape.
type RatingShape string

const (
	RatingShapeStar    RatingShape = "" // default
	RatingShapeHeart   RatingShape = "heart"
	RatingShapeThumb   RatingShape = "thumb"
	RatingShapeFire    RatingShape = "fire"
	RatingShapeDiamond RatingShape = "diamond"
	RatingShapeCircle  RatingShape = "circle"
	RatingShapeSquare  RatingShape = "square"
)

// RatingSize sets the glyph size; the tap target does not change.
type RatingSize string

const (
	RatingSizeDefault RatingSize = "" // 24px
	RatingSizeSmall   RatingSize = "small"
	RatingSizeLarge   RatingSize = "large"
)

// RatingGap sets the gap between stars, independent of Size.
type RatingGap string

const (
	RatingGapDefault RatingGap = "" // AAA 44px tap target per star
	RatingGapTight   RatingGap = "tight"
	RatingGapLoose   RatingGap = "loose"
	RatingGapWide    RatingGap = "wide"
)

// RatingConfig configures a RatingInput.
type RatingConfig struct {
	// Ctx resolves each choice's "3 out of 5" name. Nil means English.
	Ctx context.Context

	// Name and Label are required; Label names the radiogroup.
	Name  string
	Label string
	// Max defaults to 5; Value 0 means no rating chosen.
	Max   int
	Value int
	// Shape is ignored when Icon is set.
	Shape RatingShape
	// Icon replaces the bundled glyph in every star; draw it in
	// currentColor so the selected fill shows.
	Icon render.HTML
	// Size is 24px by default, 16px small and 32px large.
	Size RatingSize
	// Gap's default keeps a 44px tap target per star; Tight relaxes it
	// to the 24px floor.
	Gap      RatingGap
	Disabled bool
	ID       string
	Class    string
	// ExtraAttrs forwards attributes to the root fieldset; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs
}

var ratingClasses = headless.Classes{
	headless.PartRoot:         "fui-rating",
	headless.PartControl:      "fui-rating__input",
	headless.PartOptionChoice: "fui-rating__choice",
	headless.PartIcon:         "fui-rating__star",
}

// RatingInput renders a rating as a radio group that submits
// Name=<1..Max> with the surrounding form.
func RatingInput(cfg RatingConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: RatingInput requires Name")
	}
	if cfg.Label == "" {
		panic("ui: RatingInput requires Label")
	}
	switch cfg.Shape {
	case RatingShapeStar, RatingShapeHeart, RatingShapeThumb,
		RatingShapeFire, RatingShapeDiamond, RatingShapeCircle,
		RatingShapeSquare:
	default:
		panic("ui: RatingInput unknown Shape " + string(cfg.Shape) +
			`. Pick one of: "" (star), heart, thumb, fire, diamond, circle, square`)
	}
	switch cfg.Size {
	case RatingSizeDefault, RatingSizeSmall, RatingSizeLarge:
	default:
		panic("ui: RatingInput unknown Size " + string(cfg.Size) +
			`. Pick one of: "" (default), small, large`)
	}
	switch cfg.Gap {
	case RatingGapDefault, RatingGapTight, RatingGapLoose, RatingGapWide:
	default:
		panic("ui: RatingInput unknown Gap " + string(cfg.Gap) +
			`. Pick one of: "" (default), tight, loose, wide`)
	}

	var mods []string
	if cfg.Icon == "" && cfg.Shape != RatingShapeStar {
		mods = append(mods, "fui-rating--"+string(cfg.Shape))
	}
	if cfg.Size != RatingSizeDefault {
		mods = append(mods, "fui-rating--"+string(cfg.Size))
	}
	if cfg.Gap != RatingGapDefault {
		mods = append(mods, "fui-rating--gap-"+string(cfg.Gap))
	}
	if cfg.Disabled {
		mods = append(mods, "is-disabled")
	}
	if cfg.Class != "" {
		mods = append(mods, cfg.Class)
	}
	parts := headless.Parts{}
	if len(mods) > 0 {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.Join(mods, " ")}}
	}

	icon := cfg.Icon
	if icon == "" {
		icon = headless.Own(render.HTML(ratingIcon(cfg.Shape)))
	}

	return ratingStyle.WrapHTML(headless.Rating(headless.RatingProps{
		Name:       cfg.Name,
		Label:      cfg.Label,
		Max:        cfg.Max,
		Value:      cfg.Value,
		Icon:       icon,
		Disabled:   cfg.Disabled,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:      parts,
		Strings:    StringsFor(cfg.Ctx),
		Ctx:        cfg.Ctx,
	}, ratingClasses))
}

func ratingIcon(shape RatingShape) string {
	switch shape {
	case RatingShapeHeart:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M12 21s-7.5-4.5-9.5-9.5C1 8 3 4 6.5 4 9 4 11 6 12 7c1-1 3-3 5.5-3C21 4 23 8 21.5 11.5 19.5 16.5 12 21 12 21z"/></svg>`
	case RatingShapeThumb:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M7 21H4a1 1 0 01-1-1v-9a1 1 0 011-1h3v11zm14-10a2 2 0 00-2-2h-6.31l.95-4.57.03-.32a1.5 1.5 0 00-.44-1.06L12.17 2 5.59 8.59A2 2 0 005 10v9a2 2 0 002 2h9c.83 0 1.54-.5 1.84-1.22l3.02-7.05c.09-.23.14-.47.14-.73v-1z"/></svg>`
	case RatingShapeFire:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M13.5.67s.74 2.65.74 4.8c0 2.06-1.35 3.73-3.41 3.73-2.07 0-3.63-1.67-3.63-3.73l.03-.36C5.21 7.51 4 10.62 4 14a8 8 0 0016 0c0-4.16-2-7.88-6.5-13.33zM11.71 19c-1.78 0-3.22-1.4-3.22-3.14 0-1.62 1.05-2.76 2.81-3.12 1.77-.36 3.6-1.21 4.62-2.58.39 1.29.59 2.65.59 4.04 0 2.65-2.15 4.8-4.8 4.8z"/></svg>`
	case RatingShapeDiamond:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M19 3H5L2 9l10 12L22 9l-3-6zM6.5 5h11l1.74 3.48H4.76L6.5 5zM12 18.6L4.83 10h14.34L12 18.6z"/></svg>`
	case RatingShapeCircle:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="9"/></svg>`
	case RatingShapeSquare:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><rect x="3" y="3" width="18" height="18" rx="2"/></svg>`
	default: // star
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77 5.82 21l1.18-6.88-5-4.87 6.91-1.01z"/></svg>`
	}
}

var ratingStyle = registry.RegisterStyle("ui-rating", ratingCSS)

func ratingCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-rating"] {
  --_rating-glyph: var(--ui-rating-glyph, 24px);
  --_rating-cell: var(--ui-rating-cell, var(--spacing-touch-target, 44px));
  display: inline-flex;
  gap: var(--spacing-xs, 2px);
  margin: 0;
  padding: 0;
  border: 0;
}
[data-cui-comp="ui-rating"] .fui-rating__input {
  /* Visually hidden; clicking the label activates the input. */
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  border: 0;
  clip: rect(0,0,0,0);
  overflow: hidden;
  white-space: nowrap;
}
[data-cui-comp="ui-rating"] .fui-rating__choice {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* Block axis stays at the AAA touch-target floor; inline axis is
     driven by --ui-rating-cell which Gap variants can shrink for
     tighter density. */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--_rating-cell);
  color: var(--color-border, #E4E4E7);
  cursor: pointer;
  transition: color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), transform var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
/* Glyph (svg) size is driven by a custom property so size variants
   only have to override the property, not duplicate the rule. */
[data-cui-comp="ui-rating"] .fui-rating__star svg {
  width: var(--_rating-glyph);
  height: var(--_rating-glyph);
}
[data-cui-comp="ui-rating"].fui-rating--small { --ui-rating-glyph: 16px; }
[data-cui-comp="ui-rating"].fui-rating--large { --ui-rating-glyph: 32px; }

/* Gap presets — independent of Size.
   Default keeps the WCAG 2.5.5 AAA tap-target floor (44×44 per star).
   Tight shrinks the inline tap zone to glyph+8px so adjacent glyphs
   actually touch — the block axis stays 44px and the inline zone
   stays ≥24px (WCAG 2.5.8 AA), but AAA is intentionally relaxed for
   dense inline ratings. */
[data-cui-comp="ui-rating"].fui-rating--gap-tight {
  --_rating-cell: max(24px, calc(var(--_rating-glyph) + 8px));
  gap: 0;
}
[data-cui-comp="ui-rating"].fui-rating--gap-loose { gap: var(--spacing-md, 8px); }
[data-cui-comp="ui-rating"].fui-rating--gap-wide { gap: calc(var(--spacing-sm, 4px) * 5); }
[data-cui-comp="ui-rating"] .fui-rating__choice:hover {
  transform: scale(1.08);
}
[data-cui-comp="ui-rating"] .fui-rating__input:focus-visible + .fui-rating__choice {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
  border-radius: var(--radii-sm, 6px);
}

/* Fill: the checked choice and every choice before it (a choice with
   the checked radio after it), and likewise the hovered choice and
   every choice before it. The choices are in forward DOM order, so
   "before" is :has() over a later sibling. Color is driven by
   --ui-rating-color so per-shape variants and per-instance overrides
   can recolor without writing new highlight rules. */
[data-cui-comp="ui-rating"] .fui-rating__input:checked + .fui-rating__choice,
[data-cui-comp="ui-rating"] .fui-rating__choice:has(~ .fui-rating__input:checked),
[data-cui-comp="ui-rating"]:not(.is-disabled) .fui-rating__choice:hover,
[data-cui-comp="ui-rating"]:not(.is-disabled) .fui-rating__choice:has(~ .fui-rating__choice:hover) {
  /* Amber-600 unset, not --color-warning: the warning token is tuned
     dark for text on a chip and paints a star brown. The default lives
     in the fallback, not on the root, so a page or theme that sets
     --ui-rating-color on an ancestor reaches every rating under it; a
     shape's own colour is the default beneath it. */
  color: var(--ui-rating-color, var(--_rating-shape-color, #D97706));
}

/* Per-shape color overrides — heart / fire feel red, thumb feels
   primary, diamond feels info. Star (default) and circle / square
   stay on the default amber. */
.fui-rating--heart   { --_rating-shape-color: var(--color-danger, #B91C1C); }
.fui-rating--fire    { --_rating-shape-color: var(--color-danger, #B91C1C); }
.fui-rating--thumb   { --_rating-shape-color: var(--color-primary, #18181B); }
.fui-rating--diamond { --_rating-shape-color: var(--color-info, #1D4ED8); }

[data-cui-comp="ui-rating"].is-disabled .fui-rating__choice {
  cursor: not-allowed;
  opacity: var(--opacity-muted, 0.6);
}`
}
