package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// collapsibleStyle registers the scoped CSS for fui-collapsible so the
// host emits it for any page that renders a Collapsible.
var collapsibleStyle = registry.RegisterStyle("fui-collapsible", collapsibleCSS)

// collapsibleClasses dresses headless.Disclosure's parts in this
// package's own vocabulary.
var collapsibleClasses = headless.Classes{
	headless.PartRoot:    "fui-collapsible",
	headless.PartSummary: "fui-collapsible__summary",
	headless.PartPanel:   "fui-collapsible__content",
}

// collapsibleChevron is the stroked down-chevron mask the summary draws.
const collapsibleChevron = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M4 6l4 4 4-4' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / contain no-repeat`

func collapsibleCSS(_ style.Theme) string {
	// Token chain: --fui-* (the interactive set's host override bridge,
	// see TestFuiBridgeChainsToColorTokens) wins when a host sets it, then
	// the canonical adaptive --color-* theme, then the light literal.
	// An accordion row: a hairline under each section, no box, so a run
	// of sections reads as one list. The chevron is drawn by mask so it
	// takes the muted color and turns over when the section opens.
	return `[data-cui-comp="fui-collapsible"]{border-bottom:1px solid var(--fui-border, var(--color-border, #E4E4E7));color:var(--fui-foreground, var(--color-text, #09090B))}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary{display:flex;align-items:center;justify-content:space-between;gap:var(--spacing-lg, 16px);min-block-size:var(--spacing-touch-target, 44px);padding:var(--spacing-md, 8px) 0;cursor:pointer;font-size:var(--text-sm, .875rem);font-weight:var(--font-weight-medium);color:var(--fui-foreground, var(--color-text, #09090B));list-style:none;user-select:none}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary:hover{text-decoration:underline;text-underline-offset:4px}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary::-webkit-details-marker{display:none}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary::after{content:"";flex:none;inline-size:16px;block-size:16px;background:var(--fui-muted, var(--color-text-muted, #52525B));-webkit-mask:` + collapsibleChevron + `;mask:` + collapsibleChevron + `;transition:transform var(--duration-fast, 150ms) var(--easing-ease-out, ease)}` +
		`[data-cui-comp="fui-collapsible"][open] .fui-collapsible__summary::after{transform:rotate(180deg)}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__summary:focus-visible{outline:2px solid var(--color-text-subtle);outline-offset:2px;border-radius:var(--radii-sm, 6px)}` +
		`[data-cui-comp="fui-collapsible"] .fui-collapsible__content{padding:0 0 var(--spacing-lg, 16px);font-size:var(--text-sm, .875rem);color:var(--fui-foreground, var(--color-text, #09090B))}`
}

// ─── Collapsible ────────────────────────────────────────────────────

// CollapsibleConfig configures an expand/collapse section.
// Uses the native <details> element; the headless-disclosure module
// supplies the accessibility behaviour (Escape to close,
// aria-expanded mirroring) through the data-hui-disclosure hook.
type CollapsibleConfig struct {
	Summary string // required:  the always-visible header
	Open    bool   // optional:  start expanded (default: collapsed)
	Class   string // optional:  additional CSS classes
	ID      string // optional:  element id
	// Name groups sections into an exclusive set: sections sharing a
	// Name are the native <details name> group, so opening one closes
	// the others with no script. Empty leaves each section independent.
	Name string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root <details>. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// the data-hui-* wiring, and open (use Open).
	ExtraAttrs html.Attrs
}

// Collapsible renders a <details> element with a clickable summary.
// The data-hui-disclosure hook wires up keyboard accessibility via
// the runtime (Escape to close, aria-expanded mirroring).
//
// The body is wrapped in a fui-collapsible__content div so CSS can
// target the expandable region independently of the summary.
func Collapsible(cfg CollapsibleConfig, body ...render.HTML) render.HTML {
	classes := map[headless.Part]string{
		headless.PartRoot:    "fui-collapsible",
		headless.PartSummary: "fui-collapsible__summary",
		headless.PartPanel:   "fui-collapsible__content",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	if cfg.Summary == "" {
		panic("ui: Collapsible requires Summary — a details whose summary says nothing is a button with no name")
	}
	out := headless.Disclosure(headless.DisclosureProps{
		// Wrapped so it can carry the mark: the summary is built from
		// a string, so it is the component's own.
		Summary:    headless.Own(render.Tag("span", nil, render.Text(cfg.Summary))),
		Content:    render.Join(body...),
		Open:       cfg.Open,
		Name:       cfg.Name,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "open"),
	}, classes)
	return collapsibleStyle.WrapHTML(out)
}
