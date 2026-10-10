package ui

import (
	"context"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// DataTable is a server-rendered list view: headless.Table's structure
// dressed with this package's class map, the styled EmptyState in the
// primitive's empty slot, and the typed pager (ui.Pagination) in its
// footer slot. Sorting is typed props — the active sort, the query the
// screen carries, the parameter names — and the primitive builds every
// href through net/url, replacing the sort parameters rather than
// substituting into a caller's pattern string.

// Column describes one DataTable column.
type Column struct {
	// Key is the column identifier used for sort state and matching
	// against Row.Cells. Required.
	Key string

	// Header is the visible column header text. May be empty: an
	// actions or icon column. A sortable column with no header gets
	// its sort anchor named from the column Key.
	Header string

	// Sortable makes the header a sort control: an anchor that
	// re-submits the screen's query with this column's Key and the
	// next direction.
	Sortable bool

	// Align is "start" (default), "center", or "end". It reaches the
	// markup as the column's variant, so the class map names the
	// alignment class for the header and the cell.
	Align string

	// Wrap lets the column's cells wrap, for prose such as a note or a
	// description. Every other cell holds its value on one line, so a
	// table wider than its box scrolls sideways rather than breaking a
	// date at its hyphens. A wrapping cell keeps
	// --ui-data-table-wrap-width (16rem) as its minimum width.
	Wrap bool

	// Fit narrows the column to its content, leaving the table's spare
	// width to the data columns: a selection checkbox, a row menu. A
	// column cannot both Fit and Wrap.
	Fit bool

	// Truncate holds a long value to one line of at most
	// --ui-data-table-truncate-width (20rem), cut with an ellipsis: an
	// error message, a URL. A column cannot Truncate and Wrap or Fit.
	Truncate bool

	// SelectAll makes the header a checkbox that checks or clears the
	// row checkboxes named SelectAll in this table, and shows mixed
	// when only some are checked. Header, when set, is its accessible
	// name. A select-all column cannot sort. An empty table draws no
	// box: there is nothing to select.
	SelectAll string

	// Phone places the column's cells in the compact phone row of a
	// ResponsiveRows table. Ignored in every other mode.
	Phone PhoneSlot
}

// Row is a single rendered table row. Cells map column Key → HTML.
type Row struct {
	// Cells is a map from column Key to the rendered cell HTML.
	// Missing cells render as empty strings.
	Cells map[string]render.HTML

	// ID optionally identifies the row for ARIA / interaction. Empty
	// is fine; it just won't get an `id=` attribute.
	ID string
}

// SortDir is the direction of a sort.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

// ResponsiveMode selects how a DataTable behaves when its container
// shrinks below the configured breakpoint. Detection is **container
// query** based: the table responds to its own container's inline
// size, not the viewport, so a wide table in a narrow sidebar gets
// the responsive treatment even when the page itself is wide.
type ResponsiveMode string

const (
	// ResponsiveScroll keeps the default horizontal-scroll behavior:
	// the table stays a table; the scroll region overflows.
	ResponsiveScroll ResponsiveMode = ""

	// ResponsiveCards collapses each row into a labeled card stack
	// (header → value pairs) when the container is narrower than
	// ~640px. Column headers travel with each cell via data-label,
	// which the primitive renders for every headered column, and each
	// cell's value is one .fui-data-table__value element, so a value
	// of several parts stays together on its card line.
	ResponsiveCards ResponsiveMode = "cards"

	// ResponsiveRows collapses each row into a compact two-line row
	// when the container is narrower than 720px: the PhoneTitle cell
	// over the PhoneSubtitle cell, the PhoneMeta cell over the
	// PhoneDetail cell at the end, the PhoneLead cell (a selection box)
	// before them and the PhoneEnd cell (a row menu) after. Cells of a
	// column with no Phone slot are not drawn on a phone. One column
	// must be the PhoneTitle.
	ResponsiveRows ResponsiveMode = "rows"
)

// PhoneSlot places a column's cells in a ResponsiveRows row on a
// phone. The zero value leaves the column off the phone row.
type PhoneSlot string

const (
	PhoneNone     PhoneSlot = ""
	PhoneLead     PhoneSlot = "lead"
	PhoneTitle    PhoneSlot = "title"
	PhoneSubtitle PhoneSlot = "subtitle"
	PhoneMeta     PhoneSlot = "meta"
	PhoneDetail   PhoneSlot = "detail"
	PhoneEnd      PhoneSlot = "end"
)

// DataTableConfig configures a DataTable.
//
// Note the split between the two paginations this type touches:
// framework/pagination is the server side — it parses
// ?limit/?offset/?cursor params for the auto-generated CRUD list
// endpoints and builds cursor tokens, and never renders HTML — while
// the Pagination field takes a *PaginationConfig (this package, over
// the headless primitive) and renders the page-link nav below the
// table. A typical handler uses framework/pagination to slice the
// data, then feeds the resulting page count into this config's
// Pagination nav.
type DataTableConfig struct {
	// Columns is the column definitions. Required.
	Columns []Column

	// Rows is the rendered rows for the current page.
	Rows []Row

	// Caption is an accessible table caption (optional).
	Caption string

	// CaptionHidden keeps the caption out of sight: the caption still
	// names the table and its scroll region for assistive technology
	// (the region's aria-labelledby resolves to it) but renders
	// visually hidden, for a table that sits under a visible heading
	// saying the same thing. Requires Caption.
	CaptionHidden bool

	// SortBy is the active sort column's Key (optional). Empty means
	// no column is sorted.
	SortBy string

	// SortDir is the active sort direction (asc/desc).
	SortDir SortDir
	// Summary is a sentence about the result window the caller owns,
	// e.g. "Showing 8 of 10". Appended to the sort sentence the
	// table's announcement carries after a sort swap: the table knows
	// the sort, and only the caller knows the window.
	Summary string

	// Path is the screen's own path: each sort href is it plus the
	// carried query, the sort parameters replaced. Empty means the
	// current document — a relative "?query" href.
	Path string

	// Query is the request state sort anchors carry unchanged: the
	// search, the filters, anything a sort must not drop. The
	// primitive replaces SortParam and DirParam in it rather than
	// appending duplicate pairs.
	Query url.Values

	// SortParam and DirParam name the sort key and direction query
	// parameters. Empty defaults to "sort" and "dir".
	SortParam string
	DirParam  string

	// Island is optional. A zero value keeps plain sort anchors; a
	// set value puts the GET RPC contract on those same anchors —
	// the href is still the page without script, the region update
	// with it. The Pagination config inherits the same endpoint and
	// signal automatically.
	//
	// The signal-bound wrapper is the caller's responsibility: wrap
	// the DataTable's rendered HTML in:
	//   <div data-cui-signal="<Signal>" data-cui-signal-mode="html">
	//     {DataTable(...)}
	//   </div>
	Island headless.Island
	// Pagination is an optional *PaginationConfig. When set, the
	// pagination nav renders below the table, outside the scroll
	// region. A table with an Island shares it with the pager, so
	// sort and page hit the same handler and swap the same region.
	Pagination *PaginationConfig

	// Empty is the EmptyState shown when len(Rows) == 0, under the
	// table's head: an empty result still has named columns and
	// usable sort controls. If zero, a default empty state renders.
	Empty EmptyStateConfig

	// Responsive selects how the table behaves when its container is
	// narrow. Default keeps horizontal scroll; ResponsiveCards
	// collapses rows into labeled cards and ResponsiveRows into compact
	// two-line rows, both via container queries.
	Responsive ResponsiveMode

	// Flush drops the table's frame (border, corners, fill): rows that
	// sit inside a card, whose frame is the card's.
	Flush bool

	// Ctx carries the per-request context used to resolve i18n
	// strings (empty-state labels, sort aria-labels, pagination
	// labels). When nil, English fallbacks are returned.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the list's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style, data-cui-* and the data-hui-* hooks.
	ExtraAttrs html.Attrs
}

// dataTableClasses dresses headless.Table's parts in this package's
// own vocabulary — the names the registered ui-data-table sheet
// matches. Only the parts a selector reads: the sheet reaches the
// head, rows and cells by tag inside .fui-data-table__table, so those
// parts carry no class. Alignment travels as the column variant: the
// same is-align-* class names the header and the cell variant. The
// status span is the exception to "only what a selector reads": it
// must not be seen, and ui-visually-hidden is the recipe that hides
// it without taking it out of the accessibility tree.
var dataTableClasses = headless.Classes{
	headless.PartRoot:    "fui-data-table",
	headless.PartScroll:  "fui-data-table__scroll",
	headless.PartTable:   "fui-data-table__table",
	headless.PartCaption: "fui-data-table__caption",
	headless.PartSort:    "fui-data-table__sort",
	headless.PartEmpty:   "fui-data-table__empty",
	headless.PartStatus:  "fui-visually-hidden",
	// The select-all header draws the kit's checkbox.
	headless.PartTableSelect: "fui-choice--checkbox fui-data-table__select",
	headless.PartControl:     "fui-choice__input",

	"header--fit":          "is-fit",
	"header--center-fit":   "is-align-center is-fit",
	"header--end-fit":      "is-align-end is-fit",
	"cell--fit":            "is-fit",
	"cell--center-fit":     "is-align-center is-fit",
	"cell--end-fit":        "is-align-end is-fit",
	"header--center":       "is-align-center",
	"header--end":          "is-align-end",
	"header--center-wrap":  "is-align-center",
	"header--end-wrap":     "is-align-end",
	"cell--center":         "is-align-center",
	"cell--end":            "is-align-end",
	"cell--wrap":           "is-wrap",
	"cell--center-wrap":    "is-align-center is-wrap",
	"cell--end-wrap":       "is-align-end is-wrap",
	"cell--truncate":       "is-truncate",
	"header--end-truncate": "is-align-end",
	"cell--end-truncate":   "is-align-end is-truncate",
}

// phoneSlots are the ResponsiveRows slots a column can name.
var phoneSlots = []PhoneSlot{PhoneLead, PhoneTitle, PhoneSubtitle, PhoneMeta, PhoneDetail, PhoneEnd}

// The phone slot is the variant's last part: every alignment variant
// gains one entry per slot, the cell adding is-phone-<slot> and the
// header keeping its alignment.
func init() {
	base := map[headless.Part]string{"header--": "", "cell--": ""}
	for k, v := range dataTableClasses {
		if strings.HasPrefix(string(k), "header--") || strings.HasPrefix(string(k), "cell--") {
			base[k+"-"] = v
		}
	}
	for k, v := range base {
		for _, slot := range phoneSlots {
			key := k + "phone-" + headless.Part(slot)
			if strings.HasPrefix(string(k), "cell--") {
				dataTableClasses[key] = strings.TrimSpace(v + " is-phone-" + string(slot))
			} else if v != "" {
				dataTableClasses[key] = v
			}
		}
	}
}

// DataTable renders the table: the headless primitive's structure,
// roles and sort anchors under this package's class map, the styled
// EmptyState in the primitive's empty slot, and the typed pager in
// a footer div of its own outside the scroll region.
// cardValues wraps each cell of a cards-mode row in one value element,
// so a card line holds two parts, the column's label and the value. A
// value made of text and inline parts ("Role · billing") would otherwise
// spread across the line, one flex item per part.
func cardValues(cells map[string]render.HTML) map[string]render.HTML {
	out := make(map[string]render.HTML, len(cells))
	for k, v := range cells {
		out[k] = render.Tag("span", map[string]string{"class": "fui-data-table__value"}, v)
	}
	return out
}

func hasPhoneTitle(cols []Column) bool {
	for _, c := range cols {
		if c.Phone == PhoneTitle {
			return true
		}
	}
	return false
}

func DataTable(cfg DataTableConfig) render.HTML {
	if len(cfg.Columns) == 0 {
		panic("ui: DataTable requires at least one Column")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, c := range cfg.Columns {
		if c.Key == "" {
			panic("ui: DataTable Column requires Key")
		}
		// Header MAY be empty, common for actions / icon columns
		// where the cells are self-evidently labeled. A Sortable
		// column with an empty Header gets an aria-label derived from
		// its Key (the primitive names the anchor from
		// Strings.TableSortBy): no panic, graceful a11y degradation.
	}

	if cfg.CaptionHidden && cfg.Caption == "" {
		panic("ui: DataTableConfig.CaptionHidden without Caption — there is no caption to hide; the caption is what names the table and its scroll region for assistive technology, so set Caption or drop CaptionHidden")
	}

	cols := make([]headless.Column, len(cfg.Columns))
	for i, c := range cfg.Columns {
		var variant string
		switch c.Align {
		case "center", "end":
			variant = c.Align
		}
		if c.Wrap && c.Fit {
			panic("ui: DataTable Column " + c.Key + " sets Fit and Wrap; a fitted column holds one line")
		}
		if c.Truncate && (c.Wrap || c.Fit) {
			panic("ui: DataTable Column " + c.Key + " sets Truncate with Wrap or Fit; a truncated column is one capped line")
		}
		if c.Truncate {
			variant = strings.TrimPrefix(variant+"-truncate", "-")
		}
		if c.Wrap {
			variant = strings.TrimPrefix(variant+"-wrap", "-")
		}
		if c.Fit {
			variant = strings.TrimPrefix(variant+"-fit", "-")
		}
		selectAll := c.SelectAll
		if len(cfg.Rows) == 0 {
			selectAll = ""
		}
		cols[i] = headless.Column{Key: c.Key, Header: c.Header, Sortable: c.Sortable, SelectAll: selectAll, Variant: variant}
		if cfg.Responsive == ResponsiveRows && c.Phone != PhoneNone {
			// The slot rides the variant, so the class map names it
			// beside the column's alignment.
			cols[i].Variant = strings.TrimPrefix(cols[i].Variant+"-phone-"+string(c.Phone), "-")
		}
	}
	if cfg.Responsive == ResponsiveRows && !hasPhoneTitle(cfg.Columns) {
		panic("ui: DataTable ResponsiveRows needs one Column with Phone: PhoneTitle; the phone row has no line that names the record")
	}
	rows := make([]headless.Row, len(cfg.Rows))
	for i, r := range cfg.Rows {
		cells := r.Cells
		if cfg.Responsive == ResponsiveCards {
			cells = cardValues(r.Cells)
		}
		rows[i] = headless.Row{ID: r.ID, Cells: cells}
	}

	var empty render.HTML
	if len(cfg.Rows) == 0 {
		e := cfg.Empty
		if e.Title == "" {
			e.Title = i18nui.T(ctx, i18nui.KeyTableNoResults)
			if e.Description == "" {
				e.Description = i18nui.T(ctx, i18nui.KeyTableEmptyDesc)
			}
		}
		empty = EmptyState(e)
	}

	var footer render.HTML
	if cfg.Pagination != nil {
		// In island mode, the pagination inherits the DataTable's
		// endpoint and signal so sort and page hit the same handler.
		pag := *cfg.Pagination
		if pag.Island.Endpoint == "" && pag.Island.Signal == "" {
			pag.Island = cfg.Island
		}
		if pag.Ctx == nil {
			pag.Ctx = ctx
		}
		// The footer is built entirely from cfg.Pagination, a typed
		// config, never caller HTML: the wrapper and the pager inside
		// it are this component's own, so the boundary sits on the
		// wrapper (Pagination also marks its own list; a mark nested
		// inside an already-marked wrapper is fine).
		footer = html.Div(html.DivConfig{
			Class:      "fui-data-table__footer",
			ExtraAttrs: html.Attrs{"data-cui-internal": ""},
		}, Pagination(pag))
	}

	// The root's modifier classes travel as part attrs, which append
	// to the class map's own root class rather than replacing it.
	rootClass := cfg.Class
	switch cfg.Responsive {
	case ResponsiveCards:
		rootClass = "fui-data-table--responsive-cards " + rootClass
	case ResponsiveRows:
		rootClass = "fui-data-table--responsive-rows " + rootClass
	}
	if cfg.Flush {
		rootClass = "fui-data-table--flush " + rootClass
	}
	if len(cfg.Rows) == 0 {
		rootClass = "is-empty " + rootClass
	}
	parts := headless.Parts{}
	if rootClass = strings.TrimSpace(rootClass); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": rootClass}}
	}
	if cfg.CaptionHidden {
		// The hidden caption travels as a part attr so it APPENDS to
		// the caption's own class (fui-data-table__caption stays, the
		// visually-hidden recipe takes it out of the paint) while the
		// element, its id and its text stay for aria-labelledby.
		if parts.Attrs == nil {
			parts.Attrs = headless.PartAttrs{}
		}
		parts.Attrs[headless.PartCaption] = html.Attrs{"class": "fui-visually-hidden"}
	}

	return dataTableStyle.WrapHTML(headless.Table(headless.TableProps{
		Columns:    cols,
		Rows:       rows,
		Caption:    cfg.Caption,
		SortBy:     cfg.SortBy,
		Summary:    cfg.Summary,
		SortDir:    headless.SortDir(cfg.SortDir),
		Path:       cfg.Path,
		Query:      cfg.Query,
		SortParam:  cfg.SortParam,
		DirParam:   cfg.DirParam,
		Island:     cfg.Island,
		Empty:      empty,
		Footer:     footer,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      parts,
		Strings:    StringsFor(ctx),
	}, dataTableClasses))
}
