package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// CardVariant selects the chrome treatment. Apps extend the set with
// RegisterCardVariant; unregistered values panic at render.
type CardVariant string

const (
	CardElevated CardVariant = ""         // default: surface, shadow and radius
	CardOutlined CardVariant = "outlined" // a border instead of a shadow
	CardFlat     CardVariant = "flat"     // neither border nor shadow
	CardRow      CardVariant = "row"      // a dense linked record without elevated chrome
)

// CardConfig configures a card.
type CardConfig struct {
	Heading string // optional title in the card's header
	// HeadingContent is the heading as markup, for words filled in the
	// browser; it wins over Heading.
	HeadingContent render.HTML
	HeadingLevel   int    // the heading level, default 3
	Description    string // optional text beneath the heading
	// Header replaces the Heading and Description block.
	Header render.HTML
	Footer render.HTML // renders below the body
	// Action is a control beside the heading; it panics with Href.
	Action render.HTML
	// Href renders the whole card as one focusable link.
	Href string
	// HeadingHref links the heading and stretches that link over the
	// card, so the whole card is clickable while Action, the footer and
	// any control in the body stay their own. Needs a Heading; refused
	// with Href.
	HeadingHref string
	// Article renders the root as an <article>, for a card that stands
	// on its own as one item of a feed or grid. Refused with Href.
	Article bool
	Variant CardVariant
	// ID is the root's id; with a heading, the heading's id is "<ID>-title".
	ID    string
	Class string
	// ExtraAttrs reach the root <a> or <div>; class, id, style, href and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var cardClasses = headless.Classes{
	headless.PartRoot:          "fui-card",
	headless.PartTitle:         "fui-card__heading",
	headless.PartDesc:          "fui-card__description",
	headless.PartCardHeader:    "fui-card__header",
	headless.PartCardAction:    "fui-card__action",
	headless.PartCardBody:      "fui-card__body",
	headless.PartCardInner:     "fui-card__inner",
	headless.PartCardTitleLink: "fui-card__heading-link",
	headless.PartFooter:        "fui-card__footer",
}

// Card renders a content card: an optional header, the body and an
// optional footer. An unregistered Variant panics.
func Card(cfg CardConfig, body ...render.HTML) render.HTML {
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
	if cfg.HeadingHref != "" {
		cls = joinNonEmpty("fui-card--interactive fui-card--stretched", cls)
	}

	parts := rootClassParts(cls)
	if cfg.Header != "" {
		if parts.Slots == nil {
			parts.Slots = headless.Slots{}
		}
		parts.Slots[headless.PartCardHeader] = cfg.Header
	}

	return cardStyle.WrapHTML(headless.Card(headless.CardProps{
		Title:        cfg.Heading,
		TitleContent: cfg.HeadingContent,
		TitleTag:     titleTag,
		Desc:         cfg.Description,
		Footer:       cfg.Footer,
		Action:       cfg.Action,
		Href:         cfg.Href,
		TitleHref:    cfg.HeadingHref,
		Article:      cfg.Article,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "class", "id", "href"),
		Parts:        parts,
	}, cardClasses, body...))
}
