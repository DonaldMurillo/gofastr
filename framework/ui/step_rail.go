package ui

// StepRail: sticky-on-desktop, static-on-mobile numbered nav for
// multi-step pages (onboarding, tutorials, guided tours). Reads as
// "you are here" + "what's next." Each step is an in-page anchor;
// pair with html.Section IDs (or ui.Section auto-slugs) for the
// jumps. headless.Steps carries the list contract — the ordered
// steps, the anchors, the current step's aria-current — and this
// adapter wraps it in the complementary rail with its title and meta.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// StepRailItem is one numbered step.
type StepRailItem struct {
	// Number is the displayed ordinal (e.g. "01", "02"). Caller picks
	// the format (zero-padded vs. plain) so the visual matches the
	// step headings.
	Number string
	// Anchor is the in-page #id this rail entry jumps to.
	Anchor string
	// Label is the visible step name.
	Label string
}

// StepRailConfig configures a StepRail.
type StepRailConfig struct {
	// Title is the small heading at the top of the rail
	// (e.g. "The path", "On this page"). Optional.
	Title string
	// Items are the numbered steps, in order.
	Items []StepRailItem
	// ActiveIndex marks one step as the active one (visually
	// highlighted, aria-current="step"). Must be in [0, len(Items))
	// or -1 for "no active step". Out-of-range values panic at render
	// time so a typo (or a `slices.Index` -1 result, which is the
	// common one) is caught immediately rather than silently
	// rendering a rail with no highlight.
	ActiveIndex int
	// Meta is optional small text below the list (e.g. a "stuck?
	// open the journal" pointer).
	Meta string
	// MetaHref, when non-empty, renders Meta as a link to this URL
	// instead of plain text, so a "stuck? ask here" pointer is
	// actually clickable.
	MetaHref string
	// Class is appended to the fui-step-rail wrapper.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the rail's root <aside>
	// element. Keys the component owns are dropped: class (use
	// Class), data-fui-*, role and aria-label (derived from Title).
	ExtraAttrs html.Attrs
}

// stepRailClasses dresses headless.Steps' parts under the rail. The
// text wrapper carries no class: the row's grid gives the number its
// column and the label the rest.
var stepRailClasses = headless.Classes{
	headless.PartRoot:    "fui-step-rail__list",
	headless.PartStep:    "fui-step-rail__item",
	headless.PartStepRow: "fui-step-rail__link",
	headless.PartMarker:  "fui-step-rail__num",
	headless.PartLabel:   "fui-step-rail__label",
}

// StepRail renders the sticky numbered nav: headless.Steps under the
// rail's own class map, wrapped in the complementary <aside> the rail
// has always been.
func StepRail(cfg StepRailConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: StepRail requires at least one Item")
	}
	if cfg.ActiveIndex < -1 || cfg.ActiveIndex >= len(cfg.Items) {
		panic("ui: StepRail ActiveIndex out of range")
	}
	aria := cfg.Title
	if aria == "" {
		aria = "Page steps"
	}

	steps := make([]headless.Step, len(cfg.Items))
	for i, item := range cfg.Items {
		state := ""
		if i == cfg.ActiveIndex {
			state = "current"
		}
		steps[i] = headless.Step{
			Label: item.Label,
			Href:  "#" + item.Anchor,
			// The caller's Number is text, not markup: Marker is the
			// one slot typed render.HTML so ProgressSteps can pass an
			// SVG, and a string that arrives here unescaped would ride
			// into the aria-hidden marker raw.
			Marker: render.Text(item.Number),
			State:  state,
		}
	}
	list := headless.Steps(headless.StepsProps{Steps: steps}, stepRailClasses)

	body := []render.HTML{}
	if cfg.Title != "" {
		// A plain label, NOT a heading: the rail is a complementary
		// landmark already named by Title (aria-label below), and
		// emitting an <h6> here would inject a stray, out-of-order
		// heading into the page outline. The label keeps the visual +
		// the landmark name without polluting the heading hierarchy.
		body = append(body, html.Div(
			html.DivConfig{Class: "fui-step-rail__title"},
			render.Text(cfg.Title)))
	}
	body = append(body, list)
	if cfg.Meta != "" {
		var meta render.HTML = render.Text(cfg.Meta)
		if cfg.MetaHref != "" {
			meta = html.Link(html.LinkConfig{Href: cfg.MetaHref, Text: cfg.Meta})
		}
		body = append(body, html.Div(
			html.DivConfig{Class: "fui-step-rail__meta"}, meta))
	}

	attrs := headless.Safe(cfg.ExtraAttrs, "class", "role", "aria-label")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := joinNonEmpty("fui-step-rail", cfg.Class)
	attrs["class"] = cls
	attrs["role"] = "complementary"
	attrs["aria-label"] = aria
	return stepRailStyle.WrapHTML(render.Tag("aside", attrs, body...))
}

var stepRailStyle = registry.RegisterStyle("ui-step-rail", stepRailCSS)

func stepRailCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-step-rail"] {
  position: sticky;
  inset-block-start: var(--ui-step-rail-top, var(--spacing-xl, 24px));
  align-self: start;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-md, 8px);
  border: 1px solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft, transparent);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__title {
  margin: 0;
  font-size: var(--text-xs, 0.75rem);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--color-text-subtle, currentColor);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__link {
  display: grid;
  grid-template-columns: 32px 1fr;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  color: var(--color-text-subtle, currentColor);
  text-decoration: none;
  border-radius: var(--radii-sm, 4px);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__link:hover,
[data-fui-comp="ui-step-rail"] .fui-step-rail__link:focus-visible {
  background: var(--color-surface-soft, rgba(0,0,0,0.04));
  color: var(--color-text, currentColor);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__link[data-state="current"] {
  color: var(--color-text, currentColor);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__num {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-subtle, currentColor);
  font-variant-numeric: tabular-nums;
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__link[data-state="current"] .fui-step-rail__num {
  color: var(--ui-step-rail-active-color, var(--color-primary, currentColor));
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__label {
  font-size: var(--text-sm, 0.875rem);
}
[data-fui-comp="ui-step-rail"] .fui-step-rail__meta {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-subtle, currentColor);
  line-height: 1.5;
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
  [data-fui-comp="ui-step-rail"] {
    position: static;
  }
}`
}
