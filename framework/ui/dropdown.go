package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// DropdownAlign picks which edge of the trigger the panel lines up with.
type DropdownAlign string

const (
	// DropdownStart lines the panel up with the trigger's start edge.
	DropdownStart DropdownAlign = "start"
	// DropdownEnd lines it up with the trigger's end edge.
	DropdownEnd DropdownAlign = "end"
)

// DropdownConfig configures a Dropdown, a floating panel for content that is not commands.
type DropdownConfig struct {
	// Label is the trigger's text. Required.
	Label string
	// Icon names a registered icon drawn before the label.
	Icon string
	// Count, above zero, draws a count badge after the label.
	Count int
	// Ctx writes Count and reaches an Avatar that sets none; nil is English.
	Ctx context.Context
	// Avatar draws the trigger as that avatar alone, named by Label.
	Avatar *AvatarConfig
	// Content is the panel. Required.
	Content render.HTML
	// Align picks the panel's edge; empty is DropdownStart.
	Align DropdownAlign
	// Open renders the panel open.
	Open bool

	ID    string
	Class string
	// ExtraAttrs land on the root details; open and class are the component's.
	ExtraAttrs html.Attrs
}

// Dropdown renders a trigger button and its floating panel.
func Dropdown(cfg DropdownConfig) render.HTML {
	if strings.TrimSpace(cfg.Label) == "" {
		panic("ui: Dropdown requires Label — the trigger is a button and needs a name")
	}
	if strings.TrimSpace(string(cfg.Content)) == "" {
		panic("ui: Dropdown requires Content — an empty panel opens onto nothing")
	}
	align := cfg.Align
	switch align {
	case "":
		align = DropdownStart
	case DropdownStart, DropdownEnd:
	default:
		panic("ui: Dropdown Align must be DropdownStart or DropdownEnd, got " + strconv.Quote(string(align)))
	}
	trigger := []render.HTML{}
	summaryClass := "fui-dropmenu__trigger"
	if cfg.Avatar != nil {
		if cfg.Icon != "" || cfg.Count > 0 {
			panic("ui: Dropdown Avatar draws the trigger; drop Icon and Count")
		}
		summaryClass += " fui-dropmenu__trigger--avatar"
		av := *cfg.Avatar
		if av.Ctx == nil {
			av.Ctx = cfg.Ctx
		}
		trigger = append(trigger, Avatar(av),
			html.Span(html.TextConfig{Class: "fui-visually-hidden"}, render.Text(cfg.Label)))
	} else {
		trigger = append(trigger, dropdownTextTrigger(cfg)...)
	}
	out := headless.Disclosure(headless.DisclosureProps{
		Summary:    headless.Own(render.Join(trigger...)),
		Content:    cfg.Content,
		Open:       cfg.Open,
		Dismiss:    true,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "open", "class"),
	}, headless.Classes{
		headless.PartRoot:    cls("fui-dropmenu fui-dropmenu--"+string(align), cfg.Class),
		headless.PartSummary: summaryClass,
		headless.PartPanel:   "fui-dropmenu__panel",
	})
	return dropdownStyle.WrapHTML(out)
}

func dropdownTextTrigger(cfg DropdownConfig) []render.HTML {
	trigger := []render.HTML{}
	if cfg.Icon != "" {
		if !IconRegistered(cfg.Icon) {
			panic("ui: Dropdown Icon " + strconv.Quote(cfg.Icon) + " is not a registered icon")
		}
		trigger = append(trigger, Icon(cfg.Icon, IconConfig{Size: "16"}))
	}
	trigger = append(trigger, html.Span(html.TextConfig{Class: "fui-dropmenu__label"}, render.Text(cfg.Label)))
	if cfg.Count > 0 {
		trigger = append(trigger, html.Span(html.TextConfig{Class: "fui-dropmenu__count"},
			render.Text(i18n.Number(cfg.Ctx, cfg.Count))))
	}
	return trigger
}

var dropdownStyle = registry.RegisterStyle("ui-dropdown", dropdownCSS)

func dropdownCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-dropdown"]).fui-dropmenu {
  position: relative;
  display: inline-block;
}
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  cursor: pointer;
  list-style: none;
  user-select: none;
  white-space: nowrap;
  padding: 0 var(--spacing-lg, 16px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #09090B);
  box-shadow: var(--shadow-xs);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  min-height: var(--spacing-touch-target, 44px);
  transition: background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger::-webkit-details-marker { display: none; }
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger:hover,
[data-cui-comp="ui-dropdown"][open] > summary.fui-dropmenu__trigger {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-dropdown"] .fui-dropmenu__trigger svg { flex: none; color: var(--color-text-muted); }
/* Avatar: the avatar is the whole trigger, round, with the focus ring
   as the only chrome. A coarse pointer keeps the touch target. */
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger--avatar,
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger--avatar:hover,
[data-cui-comp="ui-dropdown"][open] > summary.fui-dropmenu__trigger--avatar {
  justify-content: center;
  padding: 0;
  min-height: 0;
  border: 0;
  border-radius: var(--radii-full, 9999px);
  background: transparent;
  box-shadow: none;
  --ui-avatar-size: 2rem;
}
@media (pointer: coarse) {
  [data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger--avatar { min-inline-size: var(--spacing-touch-target, 44px); min-block-size: var(--spacing-touch-target, 44px); }
}
[data-cui-comp="ui-dropdown"] .fui-dropmenu__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: var(--ui-dropdown-count-size, 1.25rem);
  padding: 0 var(--spacing-xs, 2px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
  font-size: var(--text-xs, 0.75rem);
  line-height: var(--ui-dropdown-count-size, 1.25rem);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-dropdown"] > .fui-dropmenu__panel {
  /* headless-disclosure shifts a panel that would leave the viewport. */
  translate: var(--hui-panel-shift, 0);
  position: absolute;
  z-index: var(--z-dropdown, 100);
  top: calc(100% + var(--spacing-sm, 4px));
  box-sizing: border-box;
  min-inline-size: min(var(--ui-dropdown-min-width, 14rem), calc(100vw - var(--spacing-2xl, 32px)));
  max-inline-size: min(var(--ui-dropdown-max-width, 28rem), calc(100vw - var(--spacing-2xl, 32px)));
  padding: var(--spacing-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #09090B);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 10px);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.10));
  animation: fui-dropmenu-in var(--duration-dropdown-enter, 120ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
[data-cui-comp="ui-dropdown"].fui-dropmenu--start > .fui-dropmenu__panel { inset-inline-start: 0; }
[data-cui-comp="ui-dropdown"].fui-dropmenu--end > .fui-dropmenu__panel { inset-inline-end: 0; }
@keyframes fui-dropmenu-in {
  from { opacity: 0; transform: translateY(-4px) scale(0.98); }
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-dropdown"] > .fui-dropmenu__panel { animation: none; }
}
`
}
