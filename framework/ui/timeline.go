package ui

// ─── Timeline ───────────────────────────────────────────────────────
//
// Vertical event list on a rail. headless.Timeline carries the
// contract — the ordered list (the order IS the content), the
// aria-hidden dots, the <time> pair a machine-readable timestamp
// makes possible — and this adapter dresses it with the fui-timeline
// class map. Used for audit logs, activity feeds, order history,
// deployment histories.

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
	// Title is the event headline (required, e.g. "Deployed v3.2.1").
	Title string
	// Meta is the optional right-aligned secondary text (e.g. a time
	// or actor: "2h ago" / "by dom"), rendered in the header row
	// after the title.
	Meta string
	// Body is the optional supporting prose / nested HTML.
	Body render.HTML
	// Variant tints the dot on the rail. Defaults to neutral.
	Variant TimelineEventVariant
}

// TimelineConfig configures a Timeline.
type TimelineConfig struct {
	Events []TimelineEvent
	ID     string
	Class  string
	// ExtraAttrs forwards additional attributes to the <ol> root.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style and data-fui-*.
	ExtraAttrs html.Attrs
}

// timelineClasses dresses headless.Timeline's parts. PartDesc maps to
// fui-timeline__detail: the muted supporting line, keyed on the part
// and never on a bare p, which would outrank the title's own rule
// whenever an event has no Meta.
var timelineClasses = headless.Classes{
	headless.PartRoot:         "fui-timeline",
	headless.PartTimelineItem: "fui-timeline__item",
	headless.PartTimelineMark: "fui-timeline__dot",
	headless.PartTimelineHead: "fui-timeline__header",
	headless.PartTimelineMeta: "fui-timeline__meta",
	headless.PartTimelineBody: "fui-timeline__content",
	headless.PartTitle:        "fui-timeline__title",
	headless.PartDesc:         "fui-timeline__detail",

	// The tone variants join the mark's own class at render (the
	// Alert shape), so each carries the modifier alone.
	headless.Part("timeline-mark--success"): "fui-timeline__dot--success",
	headless.Part("timeline-mark--warn"):    "fui-timeline__dot--warn",
	headless.Part("timeline-mark--danger"):  "fui-timeline__dot--danger",
	headless.Part("timeline-mark--info"):    "fui-timeline__dot--info",
}

// Timeline renders an ordered list of events on a vertical rail, on
// headless.Timeline.
func Timeline(cfg TimelineConfig) render.HTML {
	if len(cfg.Events) == 0 {
		panic("ui: Timeline requires at least one Event")
	}
	events := make([]headless.Event, len(cfg.Events))
	for i, e := range cfg.Events {
		if e.Title == "" {
			panic("ui: Timeline event requires Title")
		}
		switch e.Variant {
		case TimelineNeutral, TimelineSuccess, TimelineWarn, TimelineDanger, TimelineInfo:
		default:
			panic("ui: Timeline event unknown Variant " + string(e.Variant) +
				`. Pick one of: "" (neutral), success, warn, danger, info`)
		}
		// The caller's Body is markup this adapter does not compose:
		// the wrapper keeps the sheet's body rule off the primitive's
		// own children, the shape the old markup rendered.
		body := e.Body
		if body != "" {
			body = render.Tag("div", map[string]string{"class": "fui-timeline__body"}, body)
		}
		events[i] = headless.Event{
			Title: e.Title,
			Meta:  e.Meta,
			Body:  body,
			Tone:  string(e.Variant),
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
	return `[data-fui-comp="ui-timeline"] {
  display: block;
  margin: 0;
  padding: 0;
  list-style: none;
  position: relative;
}
[data-fui-comp="ui-timeline"] .fui-timeline__item {
  position: relative;
  display: grid;
  grid-template-columns: var(--spacing-lg, 16px) 1fr;
  gap: var(--spacing-md, 8px);
  padding-block-end: var(--spacing-lg, 16px);
}
[data-fui-comp="ui-timeline"] .fui-timeline__item:last-child {
  padding-block-end: 0;
}
/* Vertical rail — drawn under the dot column. Stops at the last item
   so the rail doesn't run past the final dot. */
[data-fui-comp="ui-timeline"] .fui-timeline__item::before {
  content: "";
  position: absolute;
  left: calc(var(--spacing-lg, 16px) / 2 - 1px);
  top: var(--spacing-md, 8px);
  bottom: 0;
  width: 2px;
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-timeline"] .fui-timeline__item:last-child::before {
  display: none;
}
[data-fui-comp="ui-timeline"] .fui-timeline__dot {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 999px;
  background: var(--color-text-muted, #52525B);
  border: 2px solid var(--color-background, #FFFFFF);
  margin-top: var(--spacing-xs, 2px);
  align-self: start;
  justify-self: center;
  position: relative;
  z-index: 1;
}
[data-fui-comp="ui-timeline"] .fui-timeline__content {
  display: grid;
  gap: var(--spacing-xs, 2px);
  min-width: 0;
}
[data-fui-comp="ui-timeline"] .fui-timeline__header {
  display: flex;
  gap: var(--spacing-md, 8px);
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
}
[data-fui-comp="ui-timeline"] .fui-timeline__title {
  margin: 0;
  /* The title is a p; a host's global p rule must not retune its
     metrics — the rhythm is the family's, inherited. */
  line-height: inherit;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-timeline"] .fui-timeline__meta {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-timeline"] .fui-timeline__detail {
  margin: 0;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  line-height: 1.5;
}
[data-fui-comp="ui-timeline"] .fui-timeline__body {
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  line-height: 1.5;
}

/* Variant dots — colored highlights for state changes. Scoped under
   the marker so they outrank the base dot rule at equal class
   count. */
[data-fui-comp="ui-timeline"] .fui-timeline__dot--success { background: var(--color-success, #16A34A); }
[data-fui-comp="ui-timeline"] .fui-timeline__dot--warn    { background: var(--color-warning, #D97706); }
[data-fui-comp="ui-timeline"] .fui-timeline__dot--danger  { background: var(--color-danger, #DC2626); }
[data-fui-comp="ui-timeline"] .fui-timeline__dot--info    { background: var(--color-info, #3B82F6); }`
}
