package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── FilterToolbar ──────────────────────────────────────────────────
//
// The filter/sort control strip that sits above a DataTable or card
// grid on a list screen: a row of facet controls (native <select> or a
// radio-pill group per facet), an optional search field, an optional
// sort <select>, and an Apply / Reset pair.
//
// URL-driven, server-side, JS-optional by construction. The toolbar is
// a single <form method="GET" action="<list route>">. Submitting it
// navigates to `<action>?facet=value&sort=…&q=…`; the screen's
// Load(ctx) reads those params via app.QueryFromContext and renders the
// filtered list server-side. Refresh, share, and back-button all reduce
// to "same URL → same view" with no client state — exactly the
// "URL params are the source of truth" contract in core-ui/ARCHITECTURE.md.
// Reset is a plain <a> back to the bare action, so it clears every param
// with zero JavaScript.
//
// Responsive by construction (this is the whole point — two independent
// eval apps hand-rolled this and clipped the Apply button off-screen at
// 375px). The toolbar declares itself a container (`container-type:
// inline-size`) and lays its controls out with flex-wrap + `min-inline-
// size: 0`, so as width shrinks the row degrades row → wrapped rows →
// single-column stack. Every control, including Apply/Reset, always
// stays on-screen and tappable; nothing overflows a narrow ancestor.
// Pill labels never wrap mid-label (nowrap within a pill; wrap between
// pills), so "Waiting On Customer" stays one line instead of three.
//
// Composition, not reinvention: select facets + sort compose ui.Select,
// search composes ui.SearchInput, Apply composes ui.Button (submit),
// Reset composes ui.LinkButton. The toolbar owns only its own layout CSS
// (the arrangement of its controls) and the pill-group styling.

// FacetKind selects how a facet renders its options.
type FacetKind string

const (
	// FacetSelect renders the facet as a labelled native <select>
	// (the default — best for many options / long labels).
	FacetSelect FacetKind = ""
	// FacetPills renders the facet as a wrapping radio-pill group
	// (best for a small set of short, glanceable choices).
	FacetPills FacetKind = "pills"
)

// FacetOption is one choice within a facet.
type FacetOption struct {
	// Label is the visible option text. Required.
	Label string
	// Value is the submitted value and the option's stable identifier.
	// A Value of "" is the "no filter / all" choice.
	Value string
}

// Facet is one filter dimension: a labelled group of mutually-exclusive
// options (a status filter, a plan filter, …).
type Facet struct {
	// Name is the form field name — becomes the URL query key. Required.
	Name string
	// Label is the group's accessible name (the <select> label or the
	// pill <fieldset> legend). Required.
	Label string
	// Options are the choices. Required, at least one.
	Options []FacetOption
	// Value is the currently-active Option.Value (from the URL). Empty
	// selects the "all" choice.
	Value string
	// Kind picks the render mode. FacetSelect (default) or FacetPills.
	Kind FacetKind
	// AllLabel overrides the auto-prepended "all / no filter" choice
	// (value ""). Defaults to "All <Label>" for selects and "All" for
	// pills. Ignored when an Option already declares Value "".
	AllLabel string
}

// SortOption is one choice in the sort control.
type SortOption struct {
	// Label is the visible text ("Newest", "Name A–Z"). Required.
	Label string
	// Value is the submitted sort key. Required.
	Value string
}

// FilterSearch configures the toolbar's optional search field.
type FilterSearch struct {
	// Name is the form field name (the URL query key). Required.
	Name string
	// Value is the current query text (from the URL).
	Value string
	// Placeholder overrides the default "Search…".
	Placeholder string
	// Label overrides the accessible name (default "Search").
	Label string
}

// FilterToolbarConfig configures a FilterToolbar.
type FilterToolbarConfig struct {
	// Action is the list route the form GETs to. Required.
	Action string

	// Facets are the filter dimensions, rendered left-to-right and
	// wrapping as width shrinks.
	Facets []Facet

	// Search, when non-nil, renders a search field.
	Search *FilterSearch

	// Sort, when non-empty, renders a labelled sort <select>.
	Sort []SortOption
	// SortName is the sort field's form name. Default "sort".
	SortName string
	// SortValue is the currently-selected sort Value (from the URL).
	SortValue string
	// SortLabel overrides the sort control's label. Default "Sort by".
	SortLabel string

	// ApplyLabel overrides the submit button text. Default "Apply".
	ApplyLabel string
	// ResetLabel overrides the reset link text. Default "Reset".
	ResetLabel string
	// HideReset suppresses the Reset link (e.g. when the caller renders
	// an active-filter chip bar with its own "Clear all").
	HideReset bool

	// Label is the toolbar's accessible name (search landmark
	// aria-label). Default "Filters".
	Label string

	// Ctx carries the per-request context used to resolve i18n labels
	// (Filters / Apply / Reset / Sort by / "All <label>" / Search…).
	// When nil, English fallbacks are returned.
	Ctx context.Context

	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// FilterToolbar renders the filter/sort control strip for a list screen.
func FilterToolbar(cfg FilterToolbarConfig) render.HTML {
	if cfg.Action == "" {
		panic("ui: FilterToolbar requires Action")
	}
	if len(cfg.Facets) == 0 && cfg.Search == nil && len(cfg.Sort) == 0 {
		panic("ui: FilterToolbar requires at least one of Facets, Search, or Sort")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	label := cfg.Label
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeyFilterToolbarLabel)
	}

	controls := make([]render.HTML, 0, len(cfg.Facets)+3)

	for _, f := range cfg.Facets {
		if f.Name == "" {
			panic("ui: FilterToolbar Facet requires Name")
		}
		if f.Label == "" {
			panic("ui: FilterToolbar Facet requires Label")
		}
		if len(f.Options) == 0 {
			panic("ui: FilterToolbar Facet requires at least one Option")
		}
		if f.Kind == FacetPills {
			controls = append(controls, renderPillFacet(ctx, f))
		} else {
			controls = append(controls, renderSelectFacet(ctx, f))
		}
	}

	if cfg.Search != nil {
		if cfg.Search.Name == "" {
			panic("ui: FilterToolbar Search requires Name")
		}
		controls = append(controls, renderSearchFacet(ctx, *cfg.Search))
	}

	if len(cfg.Sort) > 0 {
		controls = append(controls, renderSortFacet(ctx, cfg))
	}

	// Apply (submit) + Reset (link to the bare action → clears params).
	applyLabel := cfg.ApplyLabel
	if applyLabel == "" {
		applyLabel = i18nui.T(ctx, i18nui.KeyFilterApply)
	}
	actionsKids := []render.HTML{
		Button(ButtonConfig{
			Label:   applyLabel,
			Variant: ButtonPrimary,
			Type:    "submit",
			Class:   "ui-filter-toolbar__apply",
		}),
	}
	if !cfg.HideReset {
		resetLabel := cfg.ResetLabel
		if resetLabel == "" {
			resetLabel = i18nui.T(ctx, i18nui.KeyFilterReset)
		}
		actionsKids = append(actionsKids, LinkButton(LinkButtonConfig{
			Label:   resetLabel,
			Href:    cfg.Action,
			Variant: ButtonGhost,
			Class:   "ui-filter-toolbar__reset",
		}))
	}
	controls = append(controls, html.Div(html.DivConfig{
		Class: "ui-filter-toolbar__actions",
	}, actionsKids...))

	formAttrs := html.Attrs{
		"class":      cls("ui-filter-toolbar", cfg.Class),
		"method":     "GET",
		"action":     cfg.Action,
		"role":       "search",
		"aria-label": label,
	}
	if cfg.ID != "" {
		formAttrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		formAttrs[k] = v
	}

	return filterToolbarStyle.WrapHTML(render.Tag("form", flattenAttrs(formAttrs), controls...))
}

// renderSelectFacet composes ui.Select for a facet, prepending an
// "all" option (value "") unless one already exists.
func renderSelectFacet(ctx context.Context, f Facet) render.HTML {
	opts := make([]SelectOption, 0, len(f.Options)+1)
	if !hasEmptyValue(f.Options) {
		allLabel := f.AllLabel
		if allLabel == "" {
			allLabel = i18nui.TVars(ctx, i18nui.KeyFilterAll, map[string]string{"label": f.Label})
		}
		opts = append(opts, SelectOption{Value: "", Text: allLabel, Selected: f.Value == ""})
	}
	for _, o := range f.Options {
		opts = append(opts, SelectOption{Value: o.Value, Text: o.Label, Selected: o.Value == f.Value})
	}
	return html.Div(html.DivConfig{Class: "ui-filter-toolbar__facet"},
		Select(SelectConfig{Name: f.Name, Label: f.Label, Options: opts}))
}

// renderSortFacet composes ui.Select for the sort control.
func renderSortFacet(ctx context.Context, cfg FilterToolbarConfig) render.HTML {
	name := cfg.SortName
	if name == "" {
		name = "sort"
	}
	sortLabel := cfg.SortLabel
	if sortLabel == "" {
		sortLabel = i18nui.T(ctx, i18nui.KeyFilterSortBy)
	}
	opts := make([]SelectOption, 0, len(cfg.Sort))
	for _, o := range cfg.Sort {
		if o.Label == "" || o.Value == "" {
			panic("ui: FilterToolbar SortOption requires Label and Value")
		}
		opts = append(opts, SelectOption{Value: o.Value, Text: o.Label, Selected: o.Value == cfg.SortValue})
	}
	return html.Div(html.DivConfig{Class: "ui-filter-toolbar__facet ui-filter-toolbar__sort"},
		Select(SelectConfig{Name: name, Label: sortLabel, Options: opts}))
}

// renderSearchFacet composes ui.SearchInput (no nested <form>).
func renderSearchFacet(ctx context.Context, s FilterSearch) render.HTML {
	placeholder := s.Placeholder
	if placeholder == "" {
		placeholder = i18nui.T(ctx, i18nui.KeySearchPlaceholder)
	}
	extra := html.Attrs{}
	if s.Value != "" {
		extra["value"] = s.Value
	}
	if s.Label != "" {
		extra["aria-label"] = s.Label
	}
	return html.Div(html.DivConfig{Class: "ui-filter-toolbar__facet ui-filter-toolbar__search"},
		SearchInput(SearchInputConfig{
			Name:        s.Name,
			ID:          "filter-search-" + slug(s.Name),
			Placeholder: placeholder,
			ExtraAttrs:  extra,
		}))
}

// renderPillFacet renders a facet as a wrapping radio-pill group inside
// a labelled <fieldset>/<legend>. Native radio semantics give the caller
// keyboard nav + a single submitted value for free.
func renderPillFacet(ctx context.Context, f Facet) render.HTML {
	pills := make([]render.HTML, 0, len(f.Options)+1)
	if !hasEmptyValue(f.Options) {
		allLabel := f.AllLabel
		if allLabel == "" {
			allLabel = i18nui.T(ctx, i18nui.KeyFilterAllPlain)
		}
		pills = append(pills, pill(f.Name, FacetOption{Label: allLabel, Value: ""}, f.Value == ""))
	}
	for _, o := range f.Options {
		if o.Label == "" {
			panic("ui: FilterToolbar FacetOption requires Label")
		}
		pills = append(pills, pill(f.Name, o, o.Value == f.Value))
	}
	legend := render.Tag("legend", map[string]string{"class": "ui-filter-toolbar__legend"}, render.Text(f.Label))
	group := render.Tag("div", map[string]string{"class": "ui-filter-toolbar__pill-group"}, pills...)
	return render.Tag("fieldset",
		map[string]string{"class": "ui-filter-toolbar__facet ui-filter-toolbar__pills"},
		legend, group)
}

// pill renders one radio-pill: a <label> wrapping a visually-hidden
// <input type=radio> and a nowrap text span.
func pill(name string, o FacetOption, checked bool) render.HTML {
	inputAttrs := map[string]string{
		"type":  "radio",
		"name":  name,
		"value": o.Value,
		"class": "ui-filter-toolbar__pill-input",
		"id":    "filter-" + slug(name) + "-" + slug(o.Value+"-"+o.Label),
	}
	if checked {
		inputAttrs["checked"] = ""
	}
	return render.Tag("label", map[string]string{"class": "ui-filter-toolbar__pill"},
		render.Tag("input", inputAttrs),
		html.Span(html.TextConfig{Class: "ui-filter-toolbar__pill-text"}, render.Text(o.Label)),
	)
}

func hasEmptyValue(opts []FacetOption) bool {
	for _, o := range opts {
		if o.Value == "" {
			return true
		}
	}
	return false
}

// cls joins a base class with an optional extra class.
func cls(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}

var filterToolbarStyle = registry.RegisterStyle("ui-filter-toolbar", filterToolbarCSS)

func filterToolbarCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-filter-toolbar"] {
  container-type: inline-size;
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--spacing-md, 12px) var(--spacing-md, 12px);
  margin: 0;
  padding: 0;
  border: 0;
}

/* Each control claims a comfortable min width and grows to fill the
   row; min-inline-size:0 lets it shrink below content width so a wide
   <select> or search box never forces horizontal overflow of a narrow
   ancestor (the bug both eval apps shipped). */
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__facet {
  flex: 1 1 12rem;
  min-inline-size: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__search {
  flex: 2 1 14rem;
}
/* Pill facets prefer their natural one-line width: max-content basis
   (no grow) so a group never fragments its pills inside a cramped cell
   while horizontal space is free. Groups wrap BETWEEN each other when
   they can't fit side by side; shrink (with the pill-group's own
   flex-wrap) only kicks in when a single group is wider than the whole
   toolbar. The ≤32rem container query below overrides flex-basis to
   100%, so the mobile stack is unaffected. */
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pills {
  flex: 0 1 max-content;
}
/* ui.Search / ui.Select fill their facet cell. */
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__search [data-fui-comp="ui-search-input"] {
  display: flex;
  inline-size: 100%;
}

/* Actions cluster: pushed to the trailing edge on a wide row; wraps to
   its own line and stretches full width on a narrow one. Always stays
   on-screen and tappable — never clipped. */
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  margin-inline-start: auto;
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__actions .ui-button {
  min-block-size: var(--spacing-touch-target, 44px);
}

/* Pill facet — fieldset reset + legend as a field label. */
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__legend {
  padding: 0;
  margin-block-end: var(--spacing-xs, 4px);
  font-weight: 500;
  font-size: var(--text-sm, 0.9rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill-group {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 6px);
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* nowrap: a multi-word label stays on one line — pills wrap between
     themselves, never mid-label (the "Waiting On Customer" → 3 lines bug). */
  white-space: nowrap;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 14px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: 999px;
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.9rem);
  cursor: pointer;
  user-select: none;
  transition: background var(--duration-fast, 150ms) var(--easing-standard, ease),
              color var(--duration-fast, 150ms) var(--easing-standard, ease),
              border-color var(--duration-fast, 150ms) var(--easing-standard, ease);
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill:hover {
  color: var(--color-text, #18181B);
  border-color: var(--color-text-muted, #a1a1aa);
}
/* Visually hide the radio; the pill label is the visible control. */
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill-input {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill:has(.ui-filter-toolbar__pill-input:checked) {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  font-weight: 600;
}
[data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill:has(.ui-filter-toolbar__pill-input:focus-visible) {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}

/* Single-column stack when the toolbar itself (not the viewport) is
   narrow — correct even inside a slim sidebar on a wide screen. Every
   control, and the whole actions cluster, goes full width and Apply
   stretches so it stays an obvious, reachable tap target. */
@container (max-width: 32rem) {
  [data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__facet,
  [data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__actions {
    flex-basis: 100%;
    margin-inline-start: 0;
  }
  [data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__actions .ui-filter-toolbar__apply {
    flex: 1 1 auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-filter-toolbar"] .ui-filter-toolbar__pill { transition: none; }
}`
}
