package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/headless"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// NotificationItem is one entry in the bell dropdown.
type NotificationItem struct {
	// Title is the headline (required, e.g. "Build #4821 failed").
	Title string
	// Body is optional supporting text.
	Body string
	// Time is an optional right-aligned timestamp string ("2m ago").
	Time string
	// Href, when set, makes the entire row a link.
	Href string
	// Unread draws the row as unread.
	Unread bool
}

// NotificationBellConfig configures a NotificationBell.
type NotificationBellConfig struct {
	// Name is the paired preset.Popover's widget name, unique on the page. Required.
	Name string
	// Href is the notifications page the trigger links to without script; required, never "#".
	Href string
	// Label is the required prefix of the trigger's accessible name, before the unread count.
	Label string
	// UnreadCount is the badge's number; the badge is hidden at 0 unless SignalUnread is set.
	UnreadCount int
	// Items are the popover's rows; none renders EmptyText.
	Items []NotificationItem
	// EmptyText is shown when Items is empty. Default "No new notifications".
	EmptyText string
	// SignalUnread binds the badge text to this signal; an empty value hides it.
	SignalUnread string
	// SignalList binds the popover list's HTML to this signal; Items seed it.
	SignalList string
	// Pages, when non-empty, scopes the popover mount to those routes.
	Pages []string
	// Ctx resolves the trigger NotificationBell returns; nil uses English.
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs forwards attributes to the root anchor; class, id, data-cui-*, href, type, aria-label and aria-describedby are dropped.
	ExtraAttrs html.Attrs
}

// NotificationBellTrigger renders the bell's trigger, with ctx (not
// cfg.Ctx; nil uses English) resolving its spoken unread count. Keep
// Name the same as the mounted popover's.
func NotificationBellTrigger(ctx context.Context, cfg NotificationBellConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: NotificationBell requires Name")
	}
	if cfg.Label == "" {
		panic("ui: NotificationBell requires Label")
	}
	if cfg.Href == "" {
		panic("ui: NotificationBell requires Href — the no-script page needs the notifications page; a popover-only bell is a dead link without script")
	}
	if cfg.Href == "#" {
		panic("ui: NotificationBell Href '#' is not a destination — point it at the notifications page")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}
	var unreadBind *headless.Bind
	if cfg.SignalUnread != "" {
		unreadBind = &headless.Bind{Signal: cfg.SignalUnread}
	}
	return notificationBellStyle.WrapHTML(headless.NotificationBell(headless.NotificationBellProps{
		Href:        cfg.Href,
		Label:       cfg.Label,
		Icon:        headless.Own(render.HTML(bellIcon())),
		UnreadCount: cfg.UnreadCount,
		UnreadBind:  unreadBind,
		Opens:       cfg.Name,
		ID:          cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "href", "aria-label", "type",
			"class", "aria-describedby",
			"data-hui-notification-bell", "data-hui-notification-count", "data-hui-notification-count-fmt"),
		Parts:   parts,
		Strings: StringsFor(ctx),
	}, notificationBellClasses))
}

// NotificationBell returns a trigger snapshot resolved with cfg.Ctx and a
// builder for the paired Popover. Mount the popover once:
//
//	_, pop := ui.NotificationBell(cfg)
//	widget.MountBuilder(router, pop)
//
// Render NotificationBellTrigger(ctx, cfg) in each request's page chrome.
func NotificationBell(cfg NotificationBellConfig) (render.HTML, *widget.Builder) {
	trigger := NotificationBellTrigger(cfg.Ctx, cfg)
	slot := requestSlot{build: func(ctx context.Context) component.Component {
		emptyText := cfg.EmptyText
		if emptyText == "" {
			emptyText = i18nui.NotificationEmpty.Text(ctx)
		}
		return &notificationBellSlot{
			name:       cfg.Name,
			label:      cfg.Label,
			items:      cfg.Items,
			emptyText:  emptyText,
			signalList: cfg.SignalList,
		}
	}}
	titleID := cfg.Name + "-title"
	pb := preset.Popover(cfg.Name).
		Hidden().
		LabelledBy(titleID).
		Slot("body", slot)
	if len(cfg.Pages) > 0 {
		pb = pb.Pages(cfg.Pages...)
	}
	return trigger, pb
}

func bellIcon() string {
	return string(Icon("bell", IconConfig{Size: "20"}))
}

type notificationBellSlot struct {
	name       string
	label      string
	items      []NotificationItem
	emptyText  string
	signalList string
}

func (s *notificationBellSlot) Render() render.HTML {
	var listChildren []render.HTML
	if len(s.items) == 0 {
		listChildren = []render.HTML{
			html.Paragraph(html.TextConfig{Class: "fui-notification-bell__empty"},
				render.Text(s.emptyText)),
		}
	} else {
		rows := make([]render.HTML, 0, len(s.items))
		for _, it := range s.items {
			rows = append(rows, renderBellRow(it))
		}
		listChildren = []render.HTML{
			render.Tag("ul", map[string]string{"class": "fui-notification-bell__list"}, rows...),
		}
	}
	listAttrs := map[string]string{"class": "fui-notification-bell__body"}
	if s.signalList != "" {
		listAttrs["data-cui-signal"] = s.signalList
		listAttrs["data-cui-signal-mode"] = "html"
	}
	return render.Tag("div", map[string]string{"class": "fui-notification-bell__panel"},
		html.Heading(html.HeadingConfig{
			Level: 3,
			ID:    s.name + "-title",
			Class: "fui-notification-bell__title",
		}, render.Text(s.label)),
		render.Tag("div", listAttrs, listChildren...),
	)
}

func renderBellRow(it NotificationItem) render.HTML {
	if it.Title == "" {
		panic("ui: NotificationItem requires Title")
	}
	rowCls := "fui-notification-bell__row"
	if it.Unread {
		rowCls += " is-unread"
	}
	innerChildren := []render.HTML{
		render.Tag("div", map[string]string{"class": "fui-notification-bell__row-header"},
			html.Span(html.TextConfig{Class: "fui-notification-bell__row-title"},
				render.Text(it.Title)),
			whenStringSpan(it.Time, "fui-notification-bell__row-time"),
		),
	}
	if it.Body != "" {
		innerChildren = append(innerChildren,
			html.Paragraph(html.TextConfig{Class: "fui-notification-bell__row-body"},
				render.Text(it.Body)))
	}
	var inner render.HTML
	if it.Href != "" {
		href := urlsafe.CleanAnchor(it.Href)
		if href == "" {
			href = "#"
		}
		inner = render.Tag("a", map[string]string{
			"href":  href,
			"class": "fui-notification-bell__row-link",
		}, innerChildren...)
	} else {
		inner = render.Tag("div", map[string]string{"class": "fui-notification-bell__row-link"},
			innerChildren...)
	}
	return render.Tag("li", map[string]string{"class": rowCls}, inner)
}

func whenStringSpan(s, cls string) render.HTML {
	if s == "" {
		return render.HTML("")
	}
	return html.Span(html.TextConfig{Class: cls}, render.Text(s))
}

var _ component.Component = (*notificationBellSlot)(nil)

var notificationBellStyle = registry.RegisterStyle("ui-notification-bell", notificationBellCSS)

func notificationBellCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-notification-bell"] {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  background: transparent;
  border: 0;
  border-radius: var(--radii-md, 8px);
  color: var(--color-text, #09090B);
  cursor: pointer;
}
[data-cui-comp="ui-notification-bell"]:hover:not(.is-popover-trigger-active) {
  background: var(--color-surface-soft, #F4F4F5);
}
/* Open: the runtime's generic trigger highlight (.is-popover-trigger-active,
   primary fill + primary-fg) restated at this component's specificity.
   Without it the hover rule above won the background while the generic
   rule won the colour: a white glyph on light grey (dark: near-black on
   dark grey) the moment the popover opened under the pointer. */
[data-cui-comp="ui-notification-bell"].is-popover-trigger-active {
  background: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
}
[data-cui-comp="ui-notification-bell"]:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-notification-bell"] .fui-notification-bell__icon {
  /* The badge's containing block: anchoring it to the GLYPH's
     top-end corner (not the 44px button's) keeps the count from
     covering the bell — a badge pinned to the button covered about
     half the glyph, leaving a red "3" and a curl. */
  position: relative;
  display: inline-flex;
  line-height: 0;
}
[data-cui-comp="ui-notification-bell"] .fui-notification-bell__badge {
  position: absolute;
  /* Outward from the glyph's top-end corner: a single digit covers
     ~15% of the 20px glyph, a two-digit count ~24% (≤ a quarter),
     and the overshoot stays inside the 44px button's 12px margins. */
  inset-block-start: var(--ui-notification-bell-badge-inset-block, -12px);
  inset-inline-end: var(--ui-notification-bell-badge-inset-inline, -10px);
  box-sizing: border-box;
  min-inline-size: var(--ui-notification-bell-badge-size, 18px);
  block-size: var(--ui-notification-bell-badge-size, 18px);
  padding: 0 calc(var(--spacing-sm, 4px) * 1.25);
  border-radius: var(--radii-full, 9999px);
  /* Status token pair — the default --color-danger is the same
     #B91C1C (red-700, ≥6.4:1 vs white), so the themed value and the
     axe-safe fallback agree; themed apps recolor the badge via their
     danger slot. */
  background: var(--color-danger, #B91C1C);
  color: var(--color-primary-fg, #FFFFFF);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-bold);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: var(--stroke-thick, 2px) solid var(--color-surface, #FFFFFF);
}
/* Hide the badge when its bound signal value is empty. */
[data-cui-comp="ui-notification-bell"] .fui-notification-bell__badge:empty {
  display: none;
}

/* Popover panel — wraps the dropped notification list.
   Knobs: --ui-notification-bell-badge-inset-block (-12px) and
   -inset-inline (-10px) offset the badge off the glyph's corner;
   --ui-notification-bell-badge-size (18px) is its round body;
   --ui-notification-bell-panel-min-width (18rem) and
   --ui-notification-bell-panel-max-width (24rem) bound the panel. */
.fui-notification-bell__panel {
  display: grid;
  gap: var(--spacing-sm, 4px);
  min-inline-size: var(--ui-notification-bell-panel-min-width, 18rem);
  max-inline-size: var(--ui-notification-bell-panel-max-width, 24rem);
  padding: var(--spacing-md, 8px);
}
.fui-notification-bell__title {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #09090B);
}
.fui-notification-bell__empty {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
  text-align: center;
  padding: var(--spacing-md, 8px) 0;
}
.fui-notification-bell__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
.fui-notification-bell__row {
  margin: 0;
}
/* Unread rows sit on the soft surface in medium weight; a side stripe
   is the look the rest of the kit has dropped. */
.fui-notification-bell__row.is-unread .fui-notification-bell__row-link {
  background: var(--color-surface-soft, #F4F4F5);
  font-weight: var(--font-weight-medium);
}
.fui-notification-bell__row-link {
  display: block;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-radius: var(--radii-sm, 6px);
  color: var(--color-text, #09090B);
  text-decoration: none;
}
a.fui-notification-bell__row-link:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
.fui-notification-bell__row-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--spacing-sm, 4px);
}
.fui-notification-bell__row-title {
  font-weight: var(--font-weight-semibold);
  font-size: var(--text-sm, 0.875rem);
}
.fui-notification-bell__row-time {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
}
.fui-notification-bell__row-body {
  margin: var(--spacing-xs, 2px) 0 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
  line-height: var(--leading-snug, 1.4);
}`
}

var notificationBellClasses = headless.Classes{
	headless.PartRoot:   "fui-notification-bell",
	headless.PartIcon:   "fui-notification-bell__icon",
	headless.PartMarker: "fui-notification-bell__badge",
	headless.PartText:   "fui-notification-bell__count",
}
