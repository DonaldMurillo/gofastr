package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// StepRailItem is one numbered step.
type StepRailItem struct {
	// Number is the displayed ordinal, formatted by the caller ("01").
	Number string
	// Anchor is the in-page id this entry jumps to, without "#".
	Anchor string
	// Label is the visible step name.
	Label string
}

// StepRailConfig configures a StepRail.
type StepRailConfig struct {
	// Ctx is the default label's and step words' language; nil is English.
	Ctx context.Context
	// Title labels the rail and names its landmark. Optional.
	Title string
	// Items are the numbered steps, in order.
	Items []StepRailItem
	// ActiveIndex is the current step, or -1 for none; outside that range it panics.
	ActiveIndex int
	// Meta is optional small text below the list.
	Meta string
	// MetaHref makes Meta a link.
	MetaHref string
	// Class is appended to the fui-step-rail wrapper.
	Class string
	// ExtraAttrs land on the aside; class, data-cui-*, role and aria-label are dropped.
	ExtraAttrs html.Attrs
}

var stepRailClasses = headless.Classes{
	headless.PartRoot:    "fui-step-rail__list",
	headless.PartStep:    "fui-step-rail__item",
	headless.PartStepRow: "fui-step-rail__link",
	headless.PartMarker:  "fui-step-rail__num",
	headless.PartLabel:   "fui-step-rail__label",
}

// StepRail renders a sticky numbered nav of in-page anchors for a multi-step page.
func StepRail(cfg StepRailConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: StepRail requires at least one Item")
	}
	if cfg.ActiveIndex < -1 || cfg.ActiveIndex >= len(cfg.Items) {
		panic("ui: StepRail ActiveIndex out of range")
	}
	aria := cfg.Title
	if aria == "" {
		aria = i18nui.StepRailLabel.Text(cfg.Ctx)
	}

	steps := make([]headless.Step, len(cfg.Items))
	for i, item := range cfg.Items {
		state := ""
		if i == cfg.ActiveIndex {
			state = "current"
		}
		steps[i] = headless.Step{
			Label:  item.Label,
			Href:   "#" + item.Anchor,
			Marker: render.Text(item.Number),
			State:  state,
		}
	}
	list, err := registry.InjectAttribute(
		headless.Steps(headless.StepsProps{Steps: steps, Strings: StringsFor(cfg.Ctx)}, stepRailClasses),
		"data-cui-internal", "")
	if err != nil {
		panic(err)
	}

	body := []render.HTML{}
	if cfg.Title != "" {
		body = append(body, html.Div(
			html.DivConfig{Class: "fui-step-rail__title", ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
			render.Text(cfg.Title)))
	}
	body = append(body, list)
	if cfg.Meta != "" {
		var meta render.HTML = render.Text(cfg.Meta)
		if cfg.MetaHref != "" {
			meta = html.Link(html.LinkConfig{Href: cfg.MetaHref, Text: cfg.Meta})
		}
		body = append(body, html.Div(
			html.DivConfig{Class: "fui-step-rail__meta", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, meta))
	}

	attrs := headless.Safe(cfg.ExtraAttrs, "class", "role", "aria-label")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	base := "fui-step-rail"
	cls := joinNonEmpty(base, cfg.Class)
	attrs["class"] = cls
	attrs["role"] = "complementary"
	attrs["aria-label"] = aria
	return stepRailStyle.WrapHTML(render.Tag("aside", attrs, body...))
}

var stepRailStyle = registry.RegisterStyle("ui-step-rail", stepRailCSS)

func stepRailCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-step-rail"] {
  position: sticky;
  /* --ui-sticky-top is where sticky parts start (under a StickyHeader
     page's header); --ui-step-rail-top is the gap below it. */
  inset-block-start: calc(var(--ui-sticky-top, 0px) + var(--ui-step-rail-top, var(--spacing-xl, 24px)));
  align-self: start;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft, transparent);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__title {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text, currentColor);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__link {
  display: grid;
  grid-template-columns: 32px 1fr;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  /* Muted, not subtle: the rail sits on --color-surface-soft, where
     --color-text-subtle (tuned to 4.5:1 on --color-surface) drops to
     4.39:1 in the default light theme. */
  color: var(--color-text-muted, currentColor);
  text-decoration: none;
  border-radius: var(--radii-sm, 6px);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__link:hover,
[data-cui-comp="ui-step-rail"] .fui-step-rail__link:focus-visible {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, currentColor);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__link[data-state="current"] {
  color: var(--color-text, currentColor);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__num {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-subtle, currentColor);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__link[data-state="current"] .fui-step-rail__num {
  color: var(--ui-step-rail-active-color, var(--color-primary, currentColor));
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__label {
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-step-rail"] .fui-step-rail__meta {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, currentColor);
  line-height: var(--leading-normal, 1.5);
  /* Long URLs in the meta line must wrap rather than overrun the
     rail's narrow column. The arbitrary break is acceptable because
     the meta line is supplemental copy, not a navigation target. */
  overflow-wrap: anywhere;
  word-break: break-word;
}

/* On phones the rail can't be sticky next to body content because
   the body collapses to a single column. We drop the sticky pin so
   the rail flows inline. Hosts that want it hidden behind a
   disclosure can override .fui-step-rail with display: none in their
   mobile breakpoint. */
@media (max-width: 720px) {
  [data-cui-comp="ui-step-rail"] {
    position: static;
  }
}`
}
