package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── PricingCard ─────────────────────────────────────────────────────
//
// A marketing pricing card: plan name, headline price + period, an
// optional one-line pitch, a checked feature list, and a CTA. Compose
// several inside a ui.Grid for a pricing page. Styling lives in the
// design system (ui-pricing-card, tokens only) — callers ship no CSS.

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
	Class       string
}

// PricingCard renders a single plan card.
func PricingCard(cfg PricingCardConfig) render.HTML {
	cls := "ui-pricing-card"
	if cfg.Featured {
		cls += " ui-pricing-card--featured"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	head := []render.HTML{
		html.Heading(html.HeadingConfig{Level: 3, Class: "ui-pricing-card__name"}, render.Text(cfg.Name)),
	}
	if cfg.Featured {
		head = append([]render.HTML{html.Span(html.TextConfig{Class: "ui-pricing-card__badge"}, render.Text("Recommended"))}, head...)
	}
	if cfg.Description != "" {
		head = append(head, html.Paragraph(html.TextConfig{Class: "ui-pricing-card__desc"}, render.Text(cfg.Description)))
	}

	price := []render.HTML{html.Span(html.TextConfig{Class: "ui-pricing-card__amount"}, render.Text(cfg.Price))}
	if cfg.Period != "" {
		price = append(price, html.Span(html.TextConfig{Class: "ui-pricing-card__period"}, render.Text(cfg.Period)))
	}

	items := make([]render.HTML, 0, len(cfg.Features))
	for _, f := range cfg.Features {
		items = append(items, html.ListItem(html.ListItemConfig{Class: "ui-pricing-card__feature"}, render.Text(f)))
	}

	out := []render.HTML{
		html.Div(html.DivConfig{Class: "ui-pricing-card__head"}, head...),
		html.Div(html.DivConfig{Class: "ui-pricing-card__price"}, price...),
	}
	if len(items) > 0 {
		out = append(out, html.UnorderedList(html.ListConfig{Class: "ui-pricing-card__features"}, items...))
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
		out = append(out, LinkButton(LinkButtonConfig{Label: label, Href: cfg.CTAHref, Variant: variant, Class: "ui-pricing-card__cta"}))
	}

	return pricingCardStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, out...))
}

var pricingCardStyle = registry.RegisterStyle("ui-pricing-card", pricingCardCSS)

func pricingCardCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-pricing-card"] {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-lg, 1rem);
  padding: 1.75rem;
  background-color: var(--color-surface, #fff);
  border: 1px solid var(--color-border, #e4e4e7);
  border-radius: 14px;
  height: 100%;
}
[data-fui-comp="ui-pricing-card"].ui-pricing-card--featured {
  border-color: var(--color-primary, #4338CA);
  box-shadow: 0 0 0 1px var(--color-primary, #4338CA);
  background-color: color-mix(in srgb, var(--color-primary, #4338CA) 4%, var(--color-surface, #fff));
}
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__head { display: flex; flex-direction: column; gap: 0.35rem; }
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__badge {
  align-self: flex-start;
  font-size: var(--text-xs, 0.7rem);
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ui-pricing-card-badge-fg, var(--color-primary, #4338CA));
  background-color: color-mix(in srgb, var(--color-primary, #4338CA) 12%, transparent);
  padding: 0.15rem var(--spacing-md, 0.5rem);
  border-radius: 999px;
}
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__name {
  font-family: var(--font-heading, inherit);
  font-size: var(--text-xl, 1.25rem);
  margin: 0;
}
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__desc { margin: 0; color: var(--color-text-muted, #65657A); font-size: var(--text-sm, 0.9rem); line-height: 1.5; }
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__price { display: flex; align-items: baseline; gap: var(--spacing-sm, 0.25rem); }
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__amount {
  font-family: var(--font-heading, inherit);
  font-size: 2.25rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__period { color: var(--color-text-muted, #65657A); font-size: var(--text-base, 0.95rem); }
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__features { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.6rem; flex: 1 1 auto; }
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__feature {
  position: relative;
  padding-inline-start: 1.6rem;
  color: var(--color-text, #1B1B2A);
  font-size: var(--text-sm, 0.92rem);
  line-height: 1.45;
}
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__feature::before {
  content: "✓";
  position: absolute;
  inset-inline-start: 0;
  color: var(--color-success, #15803D);
  font-weight: 700;
}
[data-fui-comp="ui-pricing-card"] .ui-pricing-card__cta { margin-top: auto; width: 100%; text-align: center; }
`
}
