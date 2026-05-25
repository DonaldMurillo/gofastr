package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Timeline ───────────────────────────────────────────────────────
//
// Vertical event list on a rail. Each event has a dot on the rail, a
// label, an optional time/meta, and an optional body slot. Used for
// audit logs, activity feeds, order history, deployment histories.
//
// Renders as a semantic <ol> with each event in an <li>; the rail and
// dots are drawn with CSS pseudo-elements so screen readers see a
// clean ordered list, not the visual chrome.

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
	// or actor — "2h ago" / "by dom").
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
	ExtraAttrs  html.Attrs
}

// Timeline renders an ordered list of events on a vertical rail.
func Timeline(cfg TimelineConfig) render.HTML {
	if len(cfg.Events) == 0 {
		panic("ui: Timeline requires at least one Event")
	}
	cls := "ui-timeline"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	items := make([]render.HTML, 0, len(cfg.Events))
	for _, e := range cfg.Events {
		if e.Title == "" {
			panic("ui: Timeline event requires Title")
		}
		switch e.Variant {
		case TimelineNeutral, TimelineSuccess, TimelineWarn, TimelineDanger, TimelineInfo:
		default:
			panic("ui: Timeline event unknown Variant " + string(e.Variant) +
				` — pick one of: "" (neutral), success, warn, danger, info`)
		}
		itemCls := "ui-timeline__item"
		if e.Variant != TimelineNeutral {
			itemCls += " ui-timeline__item--" + string(e.Variant)
		}
		header := []render.HTML{
			html.Span(html.TextConfig{Class: "ui-timeline__title"}, render.Text(e.Title)),
		}
		if e.Meta != "" {
			header = append(header,
				html.Span(html.TextConfig{Class: "ui-timeline__meta"}, render.Text(e.Meta)))
		}
		children := []render.HTML{
			render.Tag("span", map[string]string{"class": "ui-timeline__dot", "aria-hidden": "true"}),
			render.Tag("div", map[string]string{"class": "ui-timeline__content"},
				render.Tag("div", map[string]string{"class": "ui-timeline__header"}, header...)),
		}
		if e.Body != "" {
			children = []render.HTML{
				render.Tag("span", map[string]string{"class": "ui-timeline__dot", "aria-hidden": "true"}),
				render.Tag("div", map[string]string{"class": "ui-timeline__content"},
					render.Tag("div", map[string]string{"class": "ui-timeline__header"}, header...),
					render.Tag("div", map[string]string{"class": "ui-timeline__body"}, e.Body),
				),
			}
		}
		items = append(items,
			render.Tag("li", map[string]string{"class": itemCls}, children...))
	}

	return timelineStyle.WrapHTML(render.Tag("ol", attrs, items...))
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
[data-fui-comp="ui-timeline"] .ui-timeline__item {
  position: relative;
  display: grid;
  grid-template-columns: var(--spacing-lg, 24px) 1fr;
  gap: var(--spacing-md, 12px);
  padding-block-end: var(--spacing-lg, 24px);
}
[data-fui-comp="ui-timeline"] .ui-timeline__item:last-child {
  padding-block-end: 0;
}
/* Vertical rail — drawn under the dot column. Stops at the last item
   so the rail doesn't run past the final dot. */
[data-fui-comp="ui-timeline"] .ui-timeline__item::before {
  content: "";
  position: absolute;
  left: calc(var(--spacing-lg, 24px) / 2 - 1px);
  top: var(--spacing-md, 12px);
  bottom: 0;
  width: 2px;
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-timeline"] .ui-timeline__item:last-child::before {
  display: none;
}
[data-fui-comp="ui-timeline"] .ui-timeline__dot {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 999px;
  background: var(--color-text-muted, #52525B);
  border: 2px solid var(--color-background, #FFFFFF);
  margin-top: var(--spacing-xs, 6px);
  align-self: start;
  justify-self: center;
  position: relative;
  z-index: 1;
}
[data-fui-comp="ui-timeline"] .ui-timeline__content {
  display: grid;
  gap: var(--spacing-xs, 4px);
  min-width: 0;
}
[data-fui-comp="ui-timeline"] .ui-timeline__header {
  display: flex;
  gap: var(--spacing-md, 12px);
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
}
[data-fui-comp="ui-timeline"] .ui-timeline__title {
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-timeline"] .ui-timeline__meta {
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-timeline"] .ui-timeline__body {
  color: var(--color-text-muted, #52525B);
  font-size: 0.9rem;
  line-height: 1.5;
}

/* Variant dots — colored highlights for state changes. */
.ui-timeline__item--success .ui-timeline__dot { background: var(--color-success, #16A34A); }
.ui-timeline__item--warn .ui-timeline__dot { background: var(--color-warning, #D97706); }
.ui-timeline__item--danger .ui-timeline__dot { background: var(--color-danger, #DC2626); }
.ui-timeline__item--info .ui-timeline__dot { background: var(--color-info, #3B82F6); }`
}
