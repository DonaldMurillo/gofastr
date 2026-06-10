package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── NotificationBell ───────────────────────────────────────────────
//
// Bell button + unread-count badge + paired preset.Popover that lists
// the most recent items on click. Same compositional pattern as
// Lightbox: caller mounts the Popover once at app startup, the trigger
// HTML goes anywhere on the page.
//
// Dynamic feeds (unread count and item list arriving live via SSE):
// the caller binds the badge to a signal via the SignalUnread option,
// and binds the popover slot HTML to a signal via the SignalList
// option (the runtime mirrors signal updates into the bound nodes).

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
	// Unread marks the row with a left-edge primary stripe.
	Unread bool
}

// NotificationBellConfig configures a NotificationBell.
type NotificationBellConfig struct {
	// Name is the unique widget name (required) used for the paired
	// preset.Popover. Keep page-unique.
	Name string
	// Label is the accessible label on the bell button (required,
	// e.g. "Notifications").
	Label string
	// UnreadCount renders as a badge on the bell. Hidden when 0
	// (unless SignalUnread is set — then the badge always renders
	// and the signal drives its visibility / text).
	UnreadCount int
	// Items are the entries rendered inside the popover. ≥1 recommended;
	// 0 renders the EmptyText placeholder.
	Items []NotificationItem
	// EmptyText is shown when Items is empty. Default
	// "No new notifications".
	EmptyText string
	// SignalUnread, when non-empty, binds the badge text to that
	// signal name so live updates can swap the count without a page
	// reload. The badge always renders when this is set (signal value
	// "" hides it via the empty-state CSS rule).
	SignalUnread string
	// SignalList, when non-empty, binds the popover list HTML to that
	// signal so live updates can swap the list. Items above seed the
	// SSR initial render.
	SignalList string
	// Pages, when non-empty, scopes the popover mount to those routes.
	Pages      []string
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// NotificationBell returns the bell-button trigger HTML and a
// *widget.Builder for the paired Popover. Mount the popover once:
//
//	trigger, pop := ui.NotificationBell(ui.NotificationBellConfig{...})
//	widget.Mount(r, pop.Build())
//
// Then render `trigger` in the page header / sidebar / wherever.
func NotificationBell(cfg NotificationBellConfig) (render.HTML, *widget.Builder) {
	if cfg.Name == "" {
		panic("ui: NotificationBell requires Name")
	}
	if cfg.Label == "" {
		panic("ui: NotificationBell requires Label")
	}
	emptyText := cfg.EmptyText
	if emptyText == "" {
		emptyText = "No new notifications"
	}

	cls := "ui-notification-bell"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	btnAttrs := html.Attrs{
		"type":                    "button",
		"class":                   cls,
		"aria-label":              cfg.Label,
		"data-fui-open":           cfg.Name,
		"data-fui-popover-anchor": "bottom",
	}
	if cfg.ID != "" {
		btnAttrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		btnAttrs[k] = v
	}

	// Badge — server-rendered count + optional signal-bound override.
	badgeAttrs := html.Attrs{"class": "ui-notification-bell__badge", "aria-hidden": "true"}
	var badgeChild render.HTML
	if cfg.SignalUnread != "" {
		// Always render the span when signal-bound; CSS hides on :empty.
		badgeAttrs["data-fui-signal"] = cfg.SignalUnread
		badgeChild = render.Text(formatBellCount(cfg.UnreadCount))
	} else if cfg.UnreadCount > 0 {
		badgeChild = render.Text(formatBellCount(cfg.UnreadCount))
	}

	bellChildren := []render.HTML{
		render.HTML(bellIcon()),
	}
	if badgeChild != "" || cfg.SignalUnread != "" {
		bellChildren = append(bellChildren,
			html.Span(html.TextConfig{
				Class:      "ui-notification-bell__badge",
				ExtraAttrs: badgeAttrs,
			}, badgeChild))
	}
	// SR-only count announcement — read by assistive tech when focus
	// lands on the bell.
	if cfg.UnreadCount > 0 || cfg.SignalUnread != "" {
		btnAttrs["aria-describedby"] = cfg.Name + "-count"
		bellChildren = append(bellChildren,
			html.Span(html.TextConfig{
				ID:         cfg.Name + "-count",
				Class:      "ui-visually-hidden",
				ExtraAttrs: html.Attrs{"data-fui-signal": cfg.SignalUnread},
			}, render.Text(strconv.Itoa(cfg.UnreadCount)+" unread")))
	}

	trigger := notificationBellStyle.WrapHTML(render.Tag("button", btnAttrs, bellChildren...))

	// Popover slot.
	slot := &notificationBellSlot{
		name:       cfg.Name,
		label:      cfg.Label,
		items:      cfg.Items,
		emptyText:  emptyText,
		signalList: cfg.SignalList,
	}
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

func formatBellCount(n int) string {
	if n <= 0 {
		return ""
	}
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

func bellIcon() string {
	return `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M15 17h5l-1.4-1.4A2 2 0 0118 14.2V11a6 6 0 10-12 0v3.2c0 .5-.2 1-.6 1.4L4 17h5m6 0a3 3 0 11-6 0m6 0H9" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
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
			html.Paragraph(html.TextConfig{Class: "ui-notification-bell__empty"},
				render.Text(s.emptyText)),
		}
	} else {
		rows := make([]render.HTML, 0, len(s.items))
		for _, it := range s.items {
			rows = append(rows, renderBellRow(it))
		}
		listChildren = []render.HTML{
			render.Tag("ul", map[string]string{"class": "ui-notification-bell__list"}, rows...),
		}
	}
	// If SignalList is set, wrap the list in a signal-bound div so
	// runtime swaps replace it wholesale (mode=html).
	listAttrs := map[string]string{"class": "ui-notification-bell__body"}
	if s.signalList != "" {
		listAttrs["data-fui-signal"] = s.signalList
		listAttrs["data-fui-signal-mode"] = "html"
	}
	return render.Tag("div", map[string]string{"class": "ui-notification-bell__panel"},
		html.Heading(html.HeadingConfig{
			Level: 3,
			ID:    s.name + "-title",
			Class: "ui-notification-bell__title",
		}, render.Text(s.label)),
		render.Tag("div", listAttrs, listChildren...),
	)
}

func renderBellRow(it NotificationItem) render.HTML {
	if it.Title == "" {
		panic("ui: NotificationItem requires Title")
	}
	rowCls := "ui-notification-bell__row"
	if it.Unread {
		rowCls += " is-unread"
	}
	innerChildren := []render.HTML{
		render.Tag("div", map[string]string{"class": "ui-notification-bell__row-header"},
			html.Span(html.TextConfig{Class: "ui-notification-bell__row-title"},
				render.Text(it.Title)),
			whenStringSpan(it.Time, "ui-notification-bell__row-time"),
		),
	}
	if it.Body != "" {
		innerChildren = append(innerChildren,
			html.Paragraph(html.TextConfig{Class: "ui-notification-bell__row-body"},
				render.Text(it.Body)))
	}
	var inner render.HTML
	if it.Href != "" {
		inner = render.Tag("a", map[string]string{
			"href":  it.Href,
			"class": "ui-notification-bell__row-link",
		}, innerChildren...)
	} else {
		inner = render.Tag("div", map[string]string{"class": "ui-notification-bell__row-link"},
			innerChildren...)
	}
	return render.Tag("li", map[string]string{"class": rowCls}, inner)
}

// whenStringSpan returns an empty render.HTML when s is empty, or a
// span containing s otherwise — keeps the row-header layout uniform.
func whenStringSpan(s, cls string) render.HTML {
	if s == "" {
		return render.HTML("")
	}
	return html.Span(html.TextConfig{Class: cls}, render.Text(s))
}

var _ component.Component = (*notificationBellSlot)(nil)

var notificationBellStyle = registry.RegisterStyle("ui-notification-bell", notificationBellCSS)

func notificationBellCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-notification-bell"] {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  background: transparent;
  border: 0;
  border-radius: var(--radii-md, 8px);
  color: var(--color-text, #18181B);
  cursor: pointer;
}
[data-fui-comp="ui-notification-bell"]:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-notification-bell"]:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-notification-bell"] .ui-notification-bell__badge {
  position: absolute;
  inset-block-start: 6px;
  inset-inline-end: 6px;
  min-inline-size: 18px;
  block-size: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: #B91C1C;
  color: #FFFFFF;
  font-size: 0.7rem;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 2px solid var(--color-surface, #FFFFFF);
}
/* Hide the badge when its bound signal value is empty. */
[data-fui-comp="ui-notification-bell"] .ui-notification-bell__badge:empty {
  display: none;
}

/* Popover panel — wraps the dropped notification list. */
.ui-notification-bell__panel {
  display: grid;
  gap: var(--spacing-sm, 8px);
  min-inline-size: 18rem;
  max-inline-size: 24rem;
  padding: var(--spacing-md, 12px);
}
.ui-notification-bell__title {
  margin: 0;
  font-size: 0.85rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-muted, #52525B);
}
.ui-notification-bell__empty {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
  text-align: center;
  padding: var(--spacing-md, 12px) 0;
}
.ui-notification-bell__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 2px;
}
.ui-notification-bell__row {
  margin: 0;
}
.ui-notification-bell__row.is-unread .ui-notification-bell__row-link {
  border-inline-start: 3px solid var(--color-primary, #4F46E5);
}
.ui-notification-bell__row-link {
  display: block;
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  border-radius: var(--radii-sm, 4px);
  color: var(--color-text, #18181B);
  text-decoration: none;
}
a.ui-notification-bell__row-link:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
.ui-notification-bell__row-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--spacing-sm, 8px);
}
.ui-notification-bell__row-title {
  font-weight: 600;
  font-size: 0.9rem;
}
.ui-notification-bell__row-time {
  font-size: 0.75rem;
  color: var(--color-text-muted, #52525B);
}
.ui-notification-bell__row-body {
  margin: 2px 0 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
  line-height: 1.4;
}`
}
