package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// TimelineEventVariant colors the dot on the rail.
type TimelineEventVariant string

const (
	TimelineNeutral TimelineEventVariant = ""
	TimelineSuccess TimelineEventVariant = "success"
	TimelineWarn    TimelineEventVariant = "warn"
	TimelineDanger  TimelineEventVariant = "danger"
	TimelineInfo    TimelineEventVariant = "info"
)

// TimelineEvent is one entry in the Timeline.
type TimelineEvent struct {
	// Title is the headline; exactly one of Title and Lead is set.
	Title string
	// Lead is caller markup in place of Title; the caller escapes it.
	Lead render.HTML
	// Meta is secondary text after the title.
	Meta string
	// When is the readable timestamp; Machine is its RFC 3339 form,
	// drawn as <time datetime>. An invalid pair panics.
	When    string
	Machine string
	Body    render.HTML
	Variant TimelineEventVariant
	// Icon names a registered icon drawn in the marker; an unknown name
	// draws the plain dot.
	Icon string
}

// TimelineConfig configures a Timeline.
type TimelineConfig struct {
	Events []TimelineEvent
	ID     string
	Class  string
	// ExtraAttrs land on the <ol>; class, id, style and data-cui-* are
	// the component's.
	ExtraAttrs html.Attrs
}

var timelineClasses = headless.Classes{
	headless.PartRoot:         "fui-timeline",
	headless.PartTimelineItem: "fui-timeline__item",
	headless.PartTimelineMark: "fui-timeline__dot",
	headless.PartTimelineHead: "fui-timeline__header",
	headless.PartTimelineMeta: "fui-timeline__meta",
	headless.PartTimelineTime: "fui-timeline__meta",
	headless.PartTimelineBody: "fui-timeline__content",
	headless.PartTitle:        "fui-timeline__title",
	headless.PartDesc:         "fui-timeline__detail",

	headless.Part("timeline-mark--success"): "fui-timeline__dot--success",
	headless.Part("timeline-mark--warn"):    "fui-timeline__dot--warn",
	headless.Part("timeline-mark--danger"):  "fui-timeline__dot--danger",
	headless.Part("timeline-mark--info"):    "fui-timeline__dot--info",
}

// Timeline renders events as an ordered list on a vertical rail, on
// headless.Timeline. It panics on no events or an unknown Variant.
func Timeline(cfg TimelineConfig) render.HTML {
	if len(cfg.Events) == 0 {
		panic("ui: Timeline requires at least one Event")
	}
	events := make([]headless.Event, len(cfg.Events))
	for i, e := range cfg.Events {
		if (e.Title == "") == (e.Lead == "") {
			panic("ui: Timeline event requires exactly one of Title and Lead")
		}
		switch e.Variant {
		case TimelineNeutral, TimelineSuccess, TimelineWarn, TimelineDanger, TimelineInfo:
		default:
			panic("ui: Timeline event unknown Variant " + string(e.Variant) +
				`. Pick one of: "" (neutral), success, warn, danger, info`)
		}
		body := e.Body
		if body != "" {
			body = render.Tag("div", map[string]string{"class": "fui-timeline__body"}, body)
		}
		var icon render.HTML
		if e.Icon != "" && IconRegistered(e.Icon) {
			icon = Icon(e.Icon, IconConfig{})
		}
		events[i] = headless.Event{
			Title:   e.Title,
			Lead:    e.Lead,
			Meta:    e.Meta,
			When:    e.When,
			Machine: e.Machine,
			Body:    body,
			Tone:    string(e.Variant),
			Icon:    icon,
		}
	}

	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	if cfg.Class != "" {
		attrs["class"] = cfg.Class
	}
	parts := headless.Parts{Attrs: headless.PartAttrs{headless.PartRoot: attrs}}

	return timelineStyle.WrapHTML(headless.Timeline(headless.TimelineProps{
		ID:     cfg.ID,
		Events: events,
		Parts:  parts,
	}, timelineClasses))
}

var timelineStyle = registry.RegisterStyle("ui-timeline", timelineCSS)

func timelineCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-timeline"] {
  display: block;
  margin: 0;
  padding: 0;
  list-style: none;
  position: relative;
}
[data-cui-comp="ui-timeline"] .fui-timeline__item {
  position: relative;
  display: grid;
  grid-template-columns: var(--spacing-lg, 16px) 1fr;
  gap: var(--spacing-md, 8px);
  padding-block-end: var(--spacing-lg, 16px);
}
[data-cui-comp="ui-timeline"] .fui-timeline__item:last-child {
  padding-block-end: 0;
}
/* Vertical rail — drawn under the dot column. Stops at the last item
   so the rail doesn't run past the final dot. */
[data-cui-comp="ui-timeline"] .fui-timeline__item::before {
  content: "";
  position: absolute;
  /* Centre the rail on the dot column's midpoint: (column − rail)/2.
     Knobs: --ui-timeline-rail-width (2px), --ui-timeline-dot-size
     (12px). */
  inset-inline-start: calc((var(--spacing-lg, 16px) - var(--ui-timeline-rail-width, 2px)) / 2);
  top: var(--spacing-md, 8px);
  bottom: 0;
  width: var(--ui-timeline-rail-width, 2px);
  background: var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-timeline"] .fui-timeline__item:last-child::before {
  display: none;
}
[data-cui-comp="ui-timeline"] .fui-timeline__dot {
  display: inline-block;
  width: var(--ui-timeline-dot-size, 12px);
  height: var(--ui-timeline-dot-size, 12px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-text-muted, #52525B);
  border: var(--stroke-thick, 2px) solid var(--color-background, #FFFFFF);
  /* Centre the dot on the title's first line (text-sm at the
     body's 1.5 line height). */
  margin-top: calc((var(--text-sm, 0.875rem) * 1.5 - var(--ui-timeline-dot-size, 12px)) / 2);
  align-self: start;
  justify-self: center;
  position: relative;
  z-index: 1;
}
[data-cui-comp="ui-timeline"] .fui-timeline__content {
  display: grid;
  gap: var(--spacing-xs, 2px);
  min-width: 0;
}
[data-cui-comp="ui-timeline"] .fui-timeline__header {
  display: flex;
  gap: var(--spacing-md, 8px);
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
}
[data-cui-comp="ui-timeline"] .fui-timeline__title {
  margin: 0;
  /* The title is a p; a host's global p rule must not retune its
     metrics — the rhythm is the family's, inherited. */
  line-height: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text, #09090B);
}
/* A Lead is a sentence that carries its own emphasis (a bold actor, a
   linked record): body weight, so the emphasis reads. */
[data-cui-comp="ui-timeline"] .fui-timeline__title[data-lead] {
  font-weight: var(--font-weight-normal, 400);
}
[data-cui-comp="ui-timeline"] .fui-timeline__meta {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-timeline"] .fui-timeline__detail {
  margin: 0;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-normal, 1.5);
}
[data-cui-comp="ui-timeline"] .fui-timeline__body {
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-normal, 1.5);
}

/* Variant dots — colored highlights for state changes. Scoped under
   the marker so they outrank the base dot rule at equal class
   count. */
[data-cui-comp="ui-timeline"] .fui-timeline__dot--success { background: var(--color-success, #166534); }
[data-cui-comp="ui-timeline"] .fui-timeline__dot--warn    { background: var(--color-warning, #854D0E); }
[data-cui-comp="ui-timeline"] .fui-timeline__dot--danger  { background: var(--color-danger, #B91C1C); }
[data-cui-comp="ui-timeline"] .fui-timeline__dot--info    { background: var(--color-info, #1D4ED8); }

/* An event with an Icon: the marker is a bordered circle
   (--ui-timeline-icon-size, 28px) on the surface, the icon inside it
   tinted by the variant, and the marker column and rail widen to fit. */
[data-cui-comp="ui-timeline"] .fui-timeline__item:has(> .fui-timeline__dot > .fui-icon) {
  grid-template-columns: var(--ui-timeline-icon-size, 28px) 1fr;
  gap: var(--spacing-lg, 16px);
}
[data-cui-comp="ui-timeline"] .fui-timeline__item:has(> .fui-timeline__dot > .fui-icon)::before {
  inset-inline-start: calc((var(--ui-timeline-icon-size, 28px) - var(--ui-timeline-rail-width, 2px)) / 2);
  top: var(--ui-timeline-icon-size, 28px);
}
[data-cui-comp="ui-timeline"] .fui-timeline__dot:has(> .fui-icon) {
  --ui-timeline-dot-size: var(--ui-timeline-icon-size, 28px);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-timeline"] .fui-timeline__dot:has(> .fui-icon) > .fui-icon {
  inline-size: calc(var(--ui-timeline-icon-size, 28px) / 2);
  block-size: calc(var(--ui-timeline-icon-size, 28px) / 2);
}
[data-cui-comp="ui-timeline"] .fui-timeline__dot--success:has(> .fui-icon) { color: var(--color-success, #166534); }
[data-cui-comp="ui-timeline"] .fui-timeline__dot--warn:has(> .fui-icon)    { color: var(--color-warning, #854D0E); }
[data-cui-comp="ui-timeline"] .fui-timeline__dot--danger:has(> .fui-icon)  { color: var(--color-danger, #B91C1C); }
[data-cui-comp="ui-timeline"] .fui-timeline__dot--info:has(> .fui-icon)    { color: var(--color-info, #1D4ED8); }`
}
