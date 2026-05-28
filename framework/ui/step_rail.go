package ui

// StepRail — sticky-on-desktop, static-on-mobile numbered nav for
// multi-step pages (onboarding, tutorials, guided tours). Reads as
// "you are here" + "what's next." Each step is an in-page anchor;
// pair with html.Section IDs (or ui.Section auto-slugs) for the
// jumps. The rail does not auto-track scroll position — wire to
// scrollspy if that's wanted (see core-ui/patterns/scrollspy).

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
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
	// highlighted). Must be in [0, len(Items)) or -1 for "no active
	// step" — out-of-range values panic at render time so a typo
	// (or a `slices.Index` -1 result, which is the common one) is
	// caught immediately rather than silently rendering a rail with
	// no highlight.
	ActiveIndex int
	// Meta is optional small text below the list (e.g. a "stuck?
	// open the journal" pointer).
	Meta string
	// Class is appended to the ui-step-rail wrapper.
	Class string
}

// StepRail renders the sticky numbered nav. The wrapper is an <aside>
// with role=complementary and an aria-label derived from Title (or a
// generic fallback) — the rail is a navigation landmark for AT users
// reading along.
func StepRail(cfg StepRailConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: StepRail requires at least one Item")
	}
	if cfg.ActiveIndex < -1 || cfg.ActiveIndex >= len(cfg.Items) {
		panic("ui: StepRail ActiveIndex out of range")
	}
	cls := "ui-step-rail"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	aria := cfg.Title
	if aria == "" {
		aria = "Page steps"
	}

	listItems := make([]render.HTML, 0, len(cfg.Items))
	for i, item := range cfg.Items {
		linkCls := ""
		if i == cfg.ActiveIndex {
			linkCls = "ui-step-rail__link ui-step-rail__link--active"
		} else {
			linkCls = "ui-step-rail__link"
		}
		listItems = append(listItems, html.ListItem(html.ListItemConfig{},
			html.LinkHTML(html.LinkHTMLConfig{
				Href:  "#" + item.Anchor,
				Class: linkCls,
				Content: render.Join(
					html.Span(html.TextConfig{Class: "ui-step-rail__num"}, render.Text(item.Number)),
					html.Span(html.TextConfig{Class: "ui-step-rail__label"}, render.Text(item.Label)),
				),
			}),
		))
	}

	body := []render.HTML{}
	if cfg.Title != "" {
		body = append(body, render.Tag("h6",
			map[string]string{"class": "ui-step-rail__title"},
			render.Text(cfg.Title)))
	}
	body = append(body, render.Tag("ol",
		map[string]string{"class": "ui-step-rail__list"},
		listItems...))
	if cfg.Meta != "" {
		body = append(body, html.Div(
			html.DivConfig{Class: "ui-step-rail__meta"},
			render.Text(cfg.Meta)))
	}

	return stepRailStyle.WrapHTML(render.Tag("aside",
		map[string]string{
			"class":      cls,
			"role":       "complementary",
			"aria-label": aria,
		},
		body...,
	))
}

var stepRailStyle = registry.RegisterStyle("ui-step-rail", stepRailCSS)

func stepRailCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-step-rail"] {
  position: sticky;
  inset-block-start: var(--ui-step-rail-top, var(--spacing-xl, 32px));
  align-self: start;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 16px);
  padding: var(--spacing-md, 16px);
  border: 1px solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-soft, transparent);
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__title {
  margin: 0;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--color-text-subtle, currentColor);
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 2px;
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__link {
  display: grid;
  grid-template-columns: 32px 1fr;
  align-items: center;
  gap: var(--spacing-xs, 4px);
  padding: var(--spacing-xs, 4px) var(--spacing-sm, 8px);
  color: var(--color-text-subtle, currentColor);
  text-decoration: none;
  border-radius: var(--radius-sm, 6px);
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__link:hover,
[data-fui-comp="ui-step-rail"] .ui-step-rail__link:focus-visible {
  background: var(--color-surface-soft, rgba(0,0,0,0.04));
  color: var(--color-text, currentColor);
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__link--active {
  color: var(--color-text, currentColor);
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__num {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 11px;
  color: var(--color-text-subtle, currentColor);
  font-variant-numeric: tabular-nums;
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__link--active .ui-step-rail__num {
  color: var(--ui-step-rail-active-color, var(--color-primary, currentColor));
}
[data-fui-comp="ui-step-rail"] .ui-step-rail__meta {
  font-size: 11px;
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
   disclosure can override .ui-step-rail with display: none in their
   mobile breakpoint. */
@media (max-width: 720px) {
  [data-fui-comp="ui-step-rail"] {
    position: static;
  }
}`
}
