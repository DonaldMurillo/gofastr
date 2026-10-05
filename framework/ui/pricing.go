package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── PricingCard ─────────────────────────────────────────────────────
//
// A marketing pricing card: plan name, headline price + period, an
// optional one-line pitch, a checked feature list, and a CTA. Compose
// several inside a ui.Grid for a pricing page. Styling lives in the
// design system (ui-pricing-card, tokens only). Callers ship no CSS.

// PricingCardConfig configures one plan card.
type PricingCardConfig struct {
	Name        string   // plan name, e.g. "Pro"
	Price       string   // headline price, e.g. "$99"
	Period      string   // optional period suffix, e.g. "/mo"
	Description string   // optional one-line pitch under the name
	Features    []string // checked feature list
	CTALabel    string   // CTA button label (defaults to "Choose " + Name)
	CTAHref     string   // CTA target
	Featured    bool     // highlight as the recommended plan
	ID          string
	// HeadingLevel overrides the plan-name heading level (default 3).
	// Set to 2 when cards sit directly under the page <h1> (no
	// intervening section <h2>) so axe's heading-order rule passes.
	HeadingLevel int
	Class        string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the card's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-cui-*.
	ExtraAttrs html.Attrs
}

// PricingCard renders a single plan card. Placed directly in a ui.Grid
// that holds only plans, the cards of one row share their row lines: a description that wraps
// moves every plan's price down together, so prices, feature lists and
// buttons start level across the row. Anywhere else the card lays out
// on its own.
func PricingCard(cfg PricingCardConfig) render.HTML {
	cls := "fui-pricing-card"
	if cfg.Featured {
		cls += " fui-pricing-card--featured"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	level := cfg.HeadingLevel
	if level < 1 || level > 6 {
		level = 3
	}
	head := []render.HTML{
		html.Heading(html.HeadingConfig{Level: level, Class: "fui-pricing-card__name"}, render.Text(cfg.Name)),
	}
	if cfg.Featured {
		head = append([]render.HTML{html.Span(html.TextConfig{Class: "fui-pricing-card__badge"}, render.Text("Recommended"))}, head...)
	}
	if cfg.Description != "" {
		head = append(head, html.Paragraph(html.TextConfig{Class: "fui-pricing-card__desc"}, render.Text(cfg.Description)))
	}

	price := []render.HTML{html.Span(html.TextConfig{Class: "fui-pricing-card__amount"}, render.Text(cfg.Price))}
	if cfg.Period != "" {
		price = append(price, html.Span(html.TextConfig{Class: "fui-pricing-card__period"}, render.Text(cfg.Period)))
	}

	items := make([]render.HTML, 0, len(cfg.Features))
	for _, f := range cfg.Features {
		items = append(items, html.ListItem(html.ListItemConfig{Class: "fui-pricing-card__feature"}, render.Text(f)))
	}

	// Every field PricingCard draws from is a string or a []string, so
	// the head and price groups hold none of a caller's markup — both
	// are the topmost of their own internal subtree.
	out := []render.HTML{
		html.Div(html.DivConfig{Class: "fui-pricing-card__head", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, head...),
		html.Div(html.DivConfig{Class: "fui-pricing-card__price", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, price...),
	}
	if len(items) > 0 {
		out = append(out, html.UnorderedList(html.ListConfig{Class: "fui-pricing-card__features"}, items...))
	}
	if cfg.CTALabel != "" || cfg.CTAHref != "" {
		label := cfg.CTALabel
		if label == "" {
			label = "Choose " + cfg.Name
		}
		variant := ButtonSecondary
		if cfg.Featured {
			variant = ButtonPrimary
		}
		// LinkButton's root becomes a nested tag here, never reachable as
		// one itself: collapse its own marks into a single one, the way
		// combobox and fileupload do for their own headless calls.
		out = append(out, headless.Own(LinkButton(LinkButtonConfig{Label: label, Href: cfg.CTAHref, Variant: variant, Class: "fui-pricing-card__cta"})))
	}

	return pricingCardStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, out...))
}

var pricingCardStyle = registry.RegisterStyle("ui-pricing-card", pricingCardCSS)

func pricingCardCSS(_ style.Theme) string {
	// The card's four parts (head, price, features, button) sit on four
	// rows. In a ui.Grid that holds only plans, the card borrows those
	// rows from the grid: cards in one row share the tracks, so a
	// description that wraps moves every plan's price down together
	// instead of only its own. Anywhere else (a wrapper cell, a grid that
	// mixes in other cards) its own rows give the features the slack,
	// which keeps the button at the bottom of a stretched card.
	return `[data-cui-comp="ui-pricing-card"] {
  display: grid;
  grid-template-rows: auto auto 1fr auto;
  row-gap: var(--spacing-lg, 1rem);
  padding: var(--spacing-xl, 24px);
  background-color: var(--color-surface, #fff);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-xl, 14px);
  box-shadow: var(--shadow-sm);
  /* A minimum, not a height: a flex row only stretches an item whose
     height is auto, so height: 100% left a short card short beside a
     featured one; a definite grid cell still fills either way. */
  min-block-size: 100%;
}
:where(.fui-grid:not(:has(> :not([data-cui-comp~="ui-pricing-card"])))) > [data-cui-comp="ui-pricing-card"] {
  grid-template-rows: subgrid;
  grid-row: span 4;
}
/* The recommended plan is marked by a ring alone; its surface stays the
   same as its neighbours', as a tinted fill read as a disabled tile. */
[data-cui-comp="ui-pricing-card"].fui-pricing-card--featured {
  border-color: var(--color-primary, #18181B);
  box-shadow: 0 0 0 1px var(--color-primary, #18181B), var(--shadow-sm);
}
/* The badge rides the name's line, at its end: on a line of its own it
   pushed the featured card's price below its neighbours'. */
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__head {
  grid-row: 1;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  /* Stretched to the row's tallest head, the spare height goes below
     the copy, not between the name and its description. */
  align-content: start;
  gap: 0.35rem var(--spacing-md, 8px);
}
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__head > * { grid-column: 1 / -1; }
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__head > .fui-pricing-card__name { grid-column: 1; grid-row: 1; }
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__badge {
  grid-column: 2;
  grid-row: 1;
  white-space: nowrap;
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  /* A soft secondary chip, the same as Tag and Badge: text on the soft
     surface clears 4.5:1 in both schemes. The knob overrides the text. */
  color: var(--ui-pricing-card-badge-fg, var(--color-text, #09090B));
  background-color: var(--color-surface-soft, #F4F4F5);
  padding: 2px var(--spacing-md, 8px);
  border-radius: var(--radii-md, 8px);
}
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__name {
  font-family: var(--font-heading, inherit);
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-semibold);
  margin: 0;
}
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__desc { margin: 0; color: var(--color-text-muted, #65657A); font-size: var(--text-sm, 0.875rem); line-height: 1.5; }
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__price { grid-row: 2; display: flex; align-items: baseline; gap: var(--spacing-sm, 0.25rem); }
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__amount {
  font-family: var(--font-heading, inherit);
  font-size: 2.25rem;
  font-weight: var(--font-weight-bold);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__period { color: var(--color-text-muted, #65657A); font-size: var(--text-base, 1rem); }
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__features { grid-row: 3; list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.6rem; }
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__feature {
  position: relative;
  padding-inline-start: 1.6rem;
  color: var(--color-text, #1B1B2A);
  font-size: var(--text-sm, 0.875rem);
  line-height: 1.45;
}
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__feature::before {
  content: "✓";
  position: absolute;
  inset-inline-start: 0;
  color: var(--color-success, #15803D);
}
[data-cui-comp="ui-pricing-card"] .fui-pricing-card__cta { grid-row: 4; align-self: end; width: 100%; text-align: center; }
`
}
