package ui

import (
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

var collapsibleStyle = registry.RegisterStyle("fui-collapsible", collapsibleCSS)

var collapsibleClasses = headless.Classes{
	headless.PartRoot:    "fui-collapsible",
	headless.PartSummary: "fui-collapsible__summary",
	headless.PartPanel:   "fui-collapsible__content",
}

// collapsibleChevron is the stroked down-chevron mask the summary draws.
const collapsibleChevron = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M4 6l4 4 4-4' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / contain no-repeat`

func collapsibleCSS(_ style.Theme) string {
	return `[data-cui-comp="fui-collapsible"]{border-bottom:var(--stroke-thin, 1px) solid var(--fui-border, var(--color-border, #E4E4E7));color:var(--fui-foreground, var(--color-text, #09090B))}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary{display:flex;align-items:center;justify-content:space-between;gap:var(--spacing-lg, 16px);min-block-size:var(--spacing-touch-target, 44px);padding:var(--spacing-md, 8px) 0;cursor:pointer;font-size:var(--text-sm, .875rem);font-weight:var(--font-weight-medium);color:var(--fui-foreground, var(--color-text, #09090B));list-style:none;user-select:none}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary:hover{text-decoration:underline;text-underline-offset:0.25em}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary::-webkit-details-marker{display:none}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary::after{content:"";flex:none;inline-size:var(--ui-collapsible-chevron-size, 16px);block-size:var(--ui-collapsible-chevron-size, 16px);background:var(--fui-muted, var(--color-text-muted, #52525B));-webkit-mask:` + collapsibleChevron + `;mask:` + collapsibleChevron + `;transition:transform var(--duration-fast, 150ms) var(--easing-ease-out, ease)}` +
		`[data-cui-comp="fui-collapsible"][open] .fui-collapsible__summary::after{transform:rotate(180deg)}` +
		// Forced colours paint the mask icon's background Canvas; opting out keeps it the text colour.
		`@media (forced-colors: active){[data-cui-comp="fui-collapsible"] .fui-collapsible__summary::after{forced-color-adjust:none;background:currentColor}}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary:focus-visible{outline:var(--stroke-focus, 2px) solid var(--color-text-subtle);outline-offset:var(--stroke-focus-offset, 2px);border-radius:var(--radii-sm, 6px)}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__content{padding:0 0 var(--spacing-lg, 16px);font-size:var(--text-sm, .875rem);color:var(--fui-foreground, var(--color-text, #09090B))}`
}

// CollapsibleConfig configures an expand/collapse <details> section.
type CollapsibleConfig struct {
	Summary string // required: the always-visible header
	Open    bool   // start expanded
	Class   string
	ID      string
	// Name joins sections into one native <details name> group: opening one closes the others.
	Name string
	// ExtraAttrs lands on the <details>; class, id, data-hui-* and open are dropped.
	ExtraAttrs html.Attrs
}

// Collapsible renders a <details> with a clickable summary and the body
// in a fui-collapsible__content div. It panics without Summary.
func Collapsible(cfg CollapsibleConfig, body ...render.HTML) render.HTML {
	classes := maps.Clone(collapsibleClasses)
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	if cfg.Summary == "" {
		panic("ui: Collapsible requires Summary — a details whose summary says nothing is a button with no name")
	}
	out := headless.Disclosure(headless.DisclosureProps{
		Summary:    headless.Own(render.Tag("span", nil, render.Text(cfg.Summary))),
		Content:    render.Join(body...),
		Open:       cfg.Open,
		Name:       cfg.Name,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "open"),
	}, classes)
	return collapsibleStyle.WrapHTML(out)
}
