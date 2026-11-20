package ui

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// FacetKind selects how a facet renders its options.
type FacetKind string

const (
	// FacetSelect renders a labelled native <select> (the default).
	FacetSelect FacetKind = ""
	// FacetPills renders a wrapping radio-pill group.
	FacetPills FacetKind = "pills"
)

// FacetOption is one choice within a facet.
type FacetOption struct {
	// Label is the visible option text. Required.
	Label string
	// Value is the submitted value; "" is the "all" choice.
	Value string
}

// Facet is one filter dimension: a labelled group of exclusive options.
type Facet struct {
	// Name is the field name and URL query key. Required.
	Name string
	// Label is the group's accessible name. Required.
	Label string
	// Options are the choices. Required.
	Options []FacetOption
	// Value is the active Option.Value; one no option lists draws as
	// its own choice.
	Value string
	Kind  FacetKind
	// AllLabel names the prepended "all" choice; ignored when an
	// Option has Value "".
	AllLabel string
}

// SortOption is one choice in the sort control.
type SortOption struct {
	// Label is the visible text. Required.
	Label string
	// Value is the submitted sort key. Required.
	Value string
}

// FilterSearch configures the toolbar's optional search field.
type FilterSearch struct {
	// Name is the field name and URL query key. Required.
	Name        string
	Value       string
	Placeholder string
	Label       string
	// Shortcut is a chord ("/") that focuses the search from outside a
	// text field.
	Shortcut string
}

// HiddenField is request state the toolbar form carries through a submit.
type HiddenField struct {
	// Name is the query key. Required; it panics when it names one of
	// the form's own controls.
	Name  string
	Value string
}

// FilterToolbarConfig configures a FilterToolbar.
type FilterToolbarConfig struct {
	// Action is the list route the form GETs. Required.
	Action string
	// ClientNavigation submits through the client navigator; leave it
	// false when a kept layout renders filter-dependent content.
	ClientNavigation bool
	Facets           []Facet
	Search           *FilterSearch
	Hidden           []HiddenField
	Sort             []SortOption
	// SortName defaults to "sort".
	SortName   string
	SortValue  string
	SortLabel  string
	ApplyLabel string
	ResetLabel string
	HideReset  bool
	// Compact keeps search and actions on one row in a narrow list.
	Compact bool
	// Dropdown moves the facets, sort, Extra and Apply/Reset into a
	// Filters dropdown whose badge counts the facets set plus Applied.
	Dropdown bool
	// Extra are more controls of the form, drawn after the facets.
	Extra []render.HTML
	// Applied counts the filters Extra holds. Must not be negative.
	Applied int
	// Tools are drawn at the row's end inside the form; one holding a
	// <form> panics.
	Tools []render.HTML
	// Label is the search landmark's name. Default "Filters".
	Label string
	// Ctx resolves the toolbar's strings; nil uses English.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs land on the <form>; class, id, data-cui-*, method,
	// action, role and aria-label are the component's.
	ExtraAttrs html.Attrs
}

// FilterToolbar renders a list screen's filter and sort controls as one
// GET form to Action, with Apply and a Reset link to the bare Action.
func FilterToolbar(cfg FilterToolbarConfig) render.HTML {
	if cfg.Compact {
		cfg.Class = cls(cfg.Class, "fui-filter-toolbar--compact")
	}
	if cfg.Dropdown {
		cfg.Class = cls(cfg.Class, "fui-filter-toolbar--dropdown")
	}
	if cfg.Action == "" {
		panic("ui: FilterToolbar requires Action")
	}
	if len(cfg.Facets) == 0 && cfg.Search == nil && len(cfg.Sort) == 0 && len(cfg.Extra) == 0 {
		panic("ui: FilterToolbar requires at least one of Facets, Search, Sort or Extra")
	}
	for _, t := range cfg.Tools {
		if strings.Contains(strings.ToLower(string(t)), "<form") {
			panic("ui: FilterToolbar Tools must not hold a <form>: they render inside the toolbar's form, and a nested form is dropped")
		}
	}
	if cfg.Applied < 0 {
		panic("ui: FilterToolbar Applied must not be negative")
	}
	if cfg.SortName == "" {
		cfg.SortName = "sort"
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.ID != "" {
		ctx = render.WithIDNamespace(ctx, cfg.ID)
	}
	action := urlsafe.CleanAnchor(cfg.Action)
	if action == "" {
		action = "#"
	}

	label := cfg.Label
	if label == "" {
		label = i18nui.FilterToolbarLabel.Text(ctx)
	}

	controls := make([]render.HTML, 0, len(cfg.Facets)+len(cfg.Extra)+3)
	applied := cfg.Applied

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
		if f.Value != "" {
			applied++
		}
		if f.Kind == FacetPills {
			controls = append(controls, renderPillFacet(ctx, f))
		} else {
			controls = append(controls, renderSelectFacet(ctx, f))
		}
	}

	var search render.HTML
	if cfg.Search != nil {
		if cfg.Search.Name == "" {
			panic("ui: FilterToolbar Search requires Name")
		}
		search = renderSearchFacet(ctx, *cfg.Search)
		if !cfg.Dropdown {
			controls = append(controls, search)
		}
	}

	if len(cfg.Sort) > 0 {
		controls = append(controls, renderSortFacet(ctx, cfg))
	}
	for _, x := range cfg.Extra {
		controls = append(controls, html.Div(html.DivConfig{Class: "fui-filter-toolbar__facet"}, x))
	}

	applyLabel := cfg.ApplyLabel
	if applyLabel == "" {
		applyLabel = i18nui.FilterApply.Text(ctx)
	}
	actionsKids := []render.HTML{
		Button(ButtonConfig{
			Ctx:     ctx,
			Label:   applyLabel,
			Variant: ButtonPrimary,
			Type:    "submit",
			Class:   "fui-filter-toolbar__apply",
		}),
	}
	if !cfg.HideReset {
		resetLabel := cfg.ResetLabel
		if resetLabel == "" {
			resetLabel = i18nui.FilterReset.Text(ctx)
		}
		var resetAttrs html.Attrs
		if !cfg.ClientNavigation {
			resetAttrs = html.Attrs{"data-cui-nav": "off"}
		}
		actionsKids = append(actionsKids, LinkButton(LinkButtonConfig{
			Label:      resetLabel,
			Href:       action,
			Variant:    ButtonGhost,
			Class:      "fui-filter-toolbar__reset",
			ExtraAttrs: resetAttrs,
		}))
	}
	owned := map[string]bool{}
	if cfg.Search != nil {
		owned[cfg.Search.Name] = true
	}
	if len(cfg.Sort) > 0 {
		owned[cfg.SortName] = true
	}
	for _, f := range cfg.Facets {
		owned[f.Name] = true
	}
	var hidden []render.HTML
	for _, h := range cfg.Hidden {
		if h.Name == "" {
			panic("ui: FilterToolbar HiddenField requires Name")
		}
		if owned[h.Name] {
			panic("ui: FilterToolbar Hidden name " + h.Name + " collides with one of the form's controls; the control owns the key")
		}
		hidden = append(hidden, html.Input(html.InputConfig{Type: "hidden", Name: h.Name, Value: h.Value, ExtraAttrs: html.Attrs{"data-cui-internal": ""}}))
	}
	actions := html.Div(html.DivConfig{
		Class:      "fui-filter-toolbar__actions",
		ExtraAttrs: html.Attrs{"data-cui-internal": ""},
	}, actionsKids...)
	var row []render.HTML
	if cfg.Dropdown {
		row = append(hidden, search)
		if len(controls) > 0 {
			id := ""
			if cfg.ID != "" {
				id = cfg.ID + "-filters"
			}
			row = append(row, Dropdown(DropdownConfig{
				Label: label,
				Icon:  "filter",
				Count: applied,
				Ctx:   ctx,
				ID:    id,
				Class: "fui-filter-toolbar__filters",
				Content: html.Div(html.DivConfig{
					Class:      "fui-filter-toolbar__panel",
					ExtraAttrs: html.Attrs{"data-cui-internal": ""},
				}, append(controls, actions)...),
			}))
		}
	} else {
		row = append(hidden, controls...)
		row = append(row, actions)
	}
	if len(cfg.Tools) > 0 {
		row = append(row, html.Div(html.DivConfig{Class: "fui-filter-toolbar__tools"}, cfg.Tools...))
	}

	formAttrs := html.Attrs{
		"class":      cls("fui-filter-toolbar", cfg.Class),
		"method":     "GET",
		"action":     action,
		"role":       "search",
		"aria-label": label,
	}
	if cfg.ClientNavigation {
		formAttrs["data-cui-spa"] = ""
	}
	if cfg.ID != "" {
		formAttrs["id"] = cfg.ID
	}
	maps.Copy(formAttrs, html.SafeExtraAttrs(cfg.ExtraAttrs, "action", "method", "role", "aria-label"))
	formAttrs["action"] = action

	return filterToolbarStyle.WrapHTML(render.Tag("form", flattenAttrs(formAttrs), row...))
}

func renderSelectFacet(ctx context.Context, f Facet) render.HTML {
	opts := make([]SelectOption, 0, len(f.Options)+1)
	if !hasEmptyValue(f.Options) {
		allLabel := f.AllLabel
		if allLabel == "" {
			allLabel = i18nui.FilterAll.Text(ctx, map[string]any{"label": f.Label})
		}
		opts = append(opts, SelectOption{Value: "", Text: allLabel, Selected: f.Value == ""})
	}
	for _, o := range facetOptions(f) {
		opts = append(opts, SelectOption{Value: o.Value, Text: o.Label, Selected: o.Value == f.Value})
	}
	return html.Div(html.DivConfig{Class: "fui-filter-toolbar__facet", ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
		Select(SelectConfig{Ctx: ctx, Name: f.Name, Label: f.Label, Options: opts}))
}

func facetOptions(f Facet) []FacetOption {
	if f.Value == "" || slices.ContainsFunc(f.Options, func(o FacetOption) bool { return o.Value == f.Value }) {
		return f.Options
	}
	return append(slices.Clip(f.Options), FacetOption{Label: f.Value, Value: f.Value})
}

func renderSortFacet(ctx context.Context, cfg FilterToolbarConfig) render.HTML {
	name := cfg.SortName
	sortLabel := cfg.SortLabel
	if sortLabel == "" {
		sortLabel = i18nui.FilterSortBy.Text(ctx)
	}
	opts := make([]SelectOption, 0, len(cfg.Sort))
	for _, o := range cfg.Sort {
		if o.Label == "" || o.Value == "" {
			panic("ui: FilterToolbar SortOption requires Label and Value")
		}
		opts = append(opts, SelectOption{Value: o.Value, Text: o.Label, Selected: o.Value == cfg.SortValue})
	}
	if cfg.SortValue != "" && !slices.ContainsFunc(cfg.Sort, func(o SortOption) bool { return o.Value == cfg.SortValue }) {
		opts = append(opts, SelectOption{Value: cfg.SortValue, Text: cfg.SortValue, Selected: true})
	}
	return html.Div(html.DivConfig{Class: "fui-filter-toolbar__facet fui-filter-toolbar__sort",
		ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
		Select(SelectConfig{Ctx: ctx, Name: name, Label: sortLabel, Options: opts}))
}

func renderSearchFacet(ctx context.Context, s FilterSearch) render.HTML {
	placeholder := s.Placeholder
	if placeholder == "" {
		placeholder = i18nui.SearchPlaceholder.Text(ctx)
	}
	extra := html.Attrs{}
	if s.Value != "" {
		extra["value"] = s.Value
	}
	if s.Label != "" {
		extra["aria-label"] = s.Label
	}
	id := render.UniqueID(ctx, "filter-search-"+slug(s.Name))
	wrap := html.Attrs{"data-cui-internal": ""}
	kids := []render.HTML{SearchInput(SearchInputConfig{
		Ctx:         ctx,
		Name:        s.Name,
		ID:          id,
		Placeholder: placeholder,
		ExtraAttrs:  extra,
	})}
	if s.Shortcut != "" {
		wrap["data-hui-shortcut-focus"] = s.Shortcut
		wrap["data-hui-shortcut-target"] = "#" + id
		kids = append(kids, ShortcutHint(ShortcutHintConfig{Chord: s.Shortcut, Class: "fui-filter-toolbar__search-key", Ctx: ctx}))
	}
	return html.Div(html.DivConfig{Class: "fui-filter-toolbar__facet fui-filter-toolbar__search",
		ExtraAttrs: wrap}, kids...)
}

func renderPillFacet(ctx context.Context, f Facet) render.HTML {
	pills := make([]render.HTML, 0, len(f.Options)+1)
	seen := map[string]int{}
	pillID := func(o FacetOption) string {
		id := "filter-" + slug(f.Name) + "-" + slug(o.Value+"-"+o.Label)
		seen[id]++
		if n := seen[id]; n > 1 {
			id += "-" + strconv.Itoa(n)
		}
		return render.UniqueID(ctx, id)
	}
	if !hasEmptyValue(f.Options) {
		allLabel := f.AllLabel
		if allLabel == "" {
			allLabel = i18nui.FilterAllPlain.Text(ctx)
		}
		all := FacetOption{Label: allLabel, Value: ""}
		pills = append(pills, pill(f.Name, all, f.Value == "", pillID(all)))
	}
	for _, o := range facetOptions(f) {
		if o.Label == "" {
			panic("ui: FilterToolbar FacetOption requires Label")
		}
		pills = append(pills, pill(f.Name, o, o.Value == f.Value, pillID(o)))
	}
	legend := render.Tag("legend", map[string]string{"class": "fui-filter-toolbar__legend"}, render.Text(f.Label))
	group := render.Tag("div", map[string]string{"class": "fui-filter-toolbar__pill-group"}, pills...)
	return render.Tag("fieldset",
		map[string]string{"class": "fui-filter-toolbar__facet fui-filter-toolbar__pills"},
		legend, group)
}

func pill(name string, o FacetOption, checked bool, id string) render.HTML {
	inputAttrs := map[string]string{
		"type":  "radio",
		"name":  name,
		"value": o.Value,
		"class": "fui-filter-toolbar__pill-input",
		"id":    id,
	}
	if checked {
		inputAttrs["checked"] = ""
	}
	return render.Tag("label", map[string]string{"class": "fui-filter-toolbar__pill"},
		render.Tag("input", inputAttrs),
		html.Span(html.TextConfig{Class: "fui-filter-toolbar__pill-text"}, render.Text(o.Label)),
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

func cls(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}

var filterToolbarStyle = registry.RegisterStyle("ui-filter-toolbar", filterToolbarCSS)

func filterToolbarCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-filter-toolbar"] {
  container-type: inline-size;
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--spacing-md, 8px) var(--spacing-md, 8px);
  margin: 0;
  padding: 0;
  border: 0;
}

/* Each control claims a comfortable min width and grows to fill the
   row; min-inline-size:0 lets it shrink below content width so a wide
   <select> or search box never forces horizontal overflow of a narrow
   ancestor (the bug both eval apps shipped). */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__facet {
  flex: 1 1 12rem;
  min-inline-size: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__search {
  flex: 2 1 14rem;
  position: relative;
}
/* The search's shortcut chip sits at the input's end while it is empty;
   typed text (and the clear button with it) takes the place. Pointer
   input has no use for it. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__search-key {
  position: absolute;
  inset-inline-end: var(--spacing-md, 8px);
  inset-block-start: 50%;
  transform: translateY(-50%);
  pointer-events: none;
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__search:has(input:not(:placeholder-shown)) .fui-filter-toolbar__search-key {
  display: none;
}
@media (pointer: coarse) {
  [data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__search-key { display: none; }
}
/* Pill facets prefer their natural one-line width: max-content basis
   (no grow) so a group never fragments its pills inside a cramped cell
   while horizontal space is free. Groups wrap BETWEEN each other when
   they can't fit side by side; shrink (with the pill-group's own
   flex-wrap) only kicks in when a single group is wider than the whole
   toolbar. The ≤32rem container query below overrides flex-basis to
   100%, so the mobile stack is unaffected. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pills {
  flex: 0 1 max-content;
}
/* ui.SearchInput / ui.Select fill their facet cell. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__search [data-cui-comp="ui-search-input"] {
  display: flex;
  inline-size: 100%;
}

/* Actions cluster: pushed to the trailing edge on a wide row; wraps to
   its own line and stretches full width on a narrow one. Always stays
   on-screen and tappable — never clipped. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  margin-inline-start: auto;
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__actions .fui-button {
  min-block-size: var(--spacing-touch-target, 44px);
}

/* Pill facet — fieldset reset + legend as a field label. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__legend {
  padding: 0;
  /* The same label-to-control gap a Field row uses, so the legend
     lines up with the Status and Sort labels beside it. */
  margin-block-end: var(--fui-density-gap, var(--spacing-md, 8px));
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
}
/* Segmented control: the options sit in one soft track whose outer
   height is the 44px control height, so the group lines up with the
   search field and the Apply button beside it; the checked option is a
   raised surface chip inside the track. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill-group {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-sm, 4px);
  border-radius: var(--radii-lg, 10px);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* nowrap: a multi-word label stays on one line — pills wrap between
     themselves, never mid-label (the "Waiting On Customer" → 3 lines bug). */
  white-space: nowrap;
  min-block-size: calc(var(--spacing-touch-target, 44px) - 2 * var(--spacing-sm, 4px));
  padding: 0 var(--spacing-md, 8px);
  border-radius: var(--radii-md, 8px);
  background: transparent;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  user-select: none;
  transition: background-color var(--duration-fast, 150ms), color var(--duration-fast, 150ms);
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill:hover {
  color: var(--color-text, #09090B);
}
/* Visually hide the radio; the pill label is the visible control. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill-input {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill:has(.fui-filter-toolbar__pill-input:checked) {
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #09090B);
  box-shadow: var(--shadow-xs);
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill:has(.fui-filter-toolbar__pill-input:focus-visible) {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}

/* Single-column stack when the toolbar itself (not the viewport) is
   narrow — correct even inside a slim sidebar on a wide screen. Every
   control, and the whole actions cluster, goes full width and Apply
   stretches so it stays an obvious, reachable tap target. */
@container (max-width: 32rem) {
  [data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__facet,
  [data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__actions {
    flex-basis: 100%;
    margin-inline-start: 0;
  }
  [data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__actions .fui-filter-toolbar__apply {
    flex: 1 1 auto;
  }
}
@container (min-width: 18rem) {
  [data-cui-comp="ui-filter-toolbar"].fui-filter-toolbar--compact .fui-filter-toolbar__search { flex: 1 1 0; }
  [data-cui-comp="ui-filter-toolbar"].fui-filter-toolbar--compact .fui-filter-toolbar__actions { flex: 0 0 auto; }
  [data-cui-comp="ui-filter-toolbar"].fui-filter-toolbar--compact .fui-filter-toolbar__actions .fui-filter-toolbar__apply { flex: 0 0 auto; }
}

/* Dropdown mode: the search takes a fixed share of the row
   (--ui-filter-toolbar-search-width) with the Filters button and the
   tools beside it at their natural width; on a narrow row the search
   goes full width and they wrap under it. Inside the panel the
   controls stack full width, Apply/Reset last. */
[data-cui-comp="ui-filter-toolbar"].fui-filter-toolbar--dropdown .fui-filter-toolbar__search {
  flex: 0 1 var(--ui-filter-toolbar-search-width, 22.5rem);
}
/* A compact dropdown toolbar spends its one row on search and controls.
   Keep the search flexible even in a phone-width containing block. */
[data-cui-comp="ui-filter-toolbar"].fui-filter-toolbar--dropdown.fui-filter-toolbar--compact .fui-filter-toolbar__search {
  flex: 1 1 0;
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__filters,
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__tools {
  flex: none;
}
/* The tools keep their natural width but never more than the row's,
   so on a narrow screen they wrap among themselves instead of pushing
   the page sideways. */
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__tools {
  display: flex;
  flex-wrap: wrap;
  max-inline-size: 100%;
  align-items: center;
  gap: var(--spacing-md, 8px);
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__filters {
  --ui-dropdown-min-width: var(--ui-filter-toolbar-panel-width, 20rem);
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__panel {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-lg, 16px);
  padding: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__panel .fui-filter-toolbar__facet,
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__panel .fui-filter-toolbar__actions {
  flex: none;
  margin-inline-start: 0;
}
[data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__panel .fui-filter-toolbar__actions {
  justify-content: flex-end;
  padding-block-start: var(--spacing-md, 8px);
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-filter-toolbar"] .fui-filter-toolbar__pill { transition: none; }
}`
}
