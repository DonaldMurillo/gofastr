package ui

// ─── Card ───────────────────────────────────────────────────────────
//
// headless.Card carries the structure: header (title + description,
// or a caller's whole header through the fillable part), body, footer,
// and the interactive shell where Href makes the whole surface one
// link. This adapter dresses it with the fui-card class map and this
// package's variant vocabulary.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// CardVariant selects the chrome treatment. Apps extend the set with
// RegisterCardVariant; unregistered values panic at render.
type CardVariant string

const (
	// CardElevated is the default: surface + shadow + radius.
	CardElevated CardVariant = ""
	// CardOutlined draws a 1px border instead of a shadow.
	CardOutlined CardVariant = "outlined"
	// CardFlat drops both the border and the shadow.
	CardFlat CardVariant = "flat"
)

// CardConfig configures a card.
type CardConfig struct {
	// Heading is the optional top-of-card title, rendered as a
	// heading inside the card's header. The heading level defaults to
	// 3; a card does not know how deep in the outline it sits, so a
	// page that nests cards under an h2 says HeadingLevel: 2.
	Heading string

	// HeadingLevel overrides the heading element level (default 3).
	HeadingLevel int

	// Description is optional supporting text rendered beneath the
	// heading.
	Description string

	// Header replaces the auto-rendered Heading/Description block
	// through the primitive's fillable header part. Use when the
	// header needs more than a title, e.g. a row with an avatar and
	// trailing actions.
	Header render.HTML

	// Footer renders below the body, separated by a hairline border.
	// Common usage: button row, last-updated timestamp, status pill.
	Footer render.HTML

	// Interactive flips the surface to a focusable, hover-able link
	// shell. When set, the card renders as an <a> wrapping one inner
	// part so the entire surface activates on click.
	Href string

	Variant CardVariant
	ID      string
	Class   string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the card's root element,
	// whichever shape it takes (<a> or <div>). Keys the component
	// owns are dropped: class and id (use Class / ID), style,
	// data-fui-* and href (use Href).
	ExtraAttrs html.Attrs
}

// cardClasses dresses headless.Card's parts.
var cardClasses = headless.Classes{
	headless.PartRoot:       "fui-card",
	headless.PartTitle:      "fui-card__heading",
	headless.PartDesc:       "fui-card__description",
	headless.PartCardHeader: "fui-card__header",
	headless.PartCardBody:   "fui-card__body",
	headless.PartCardInner:  "fui-card__inner",
	headless.PartFooter:     "fui-card__footer",
}

// Card renders a labelled content card on headless.Card: an optional
// titled header (or the caller's whole header through the fillable
// part), the body, an optional footer — and with Href, the whole
// surface as one focusable link.
func Card(cfg CardConfig, body ...render.HTML) render.HTML {
	// Unknown variants panic like every other variant-taking component
	// (registered custom variants pass. See RegisterCardVariant).
	checkCardVariant(cfg.Variant)

	titleTag := ""
	if cfg.HeadingLevel > 0 {
		titleTag = "h" + string(rune('0'+cfg.HeadingLevel))
	}

	cls := cfg.Class
	if cfg.Variant != CardElevated {
		cls = joinNonEmpty("fui-card--"+string(cfg.Variant), cls)
	}
	if cfg.Href != "" {
		cls = joinNonEmpty("fui-card--interactive", cls)
	}

	parts := rootClassParts(cls)
	if cfg.Header != "" {
		if parts.Slots == nil {
			parts.Slots = headless.Slots{}
		}
		parts.Slots[headless.PartCardHeader] = cfg.Header
	}

	return cardStyle.WrapHTML(headless.Card(headless.CardProps{
		Title:      cfg.Heading,
		TitleTag:   titleTag,
		Desc:       cfg.Description,
		Footer:     cfg.Footer,
		Href:       cfg.Href,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "href"),
		Parts:      parts,
	}, cardClasses, body...))
}
