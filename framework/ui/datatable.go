package ui

import (
	"context"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// DataTable is a server-rendered list view: headless.Table's structure
// dressed with this package's class map, the styled EmptyState in the
// primitive's empty slot, and the core-ui pager in its footer slot.
// Sorting is typed props — the active sort, the query the screen
// carries, the parameter names — and the primitive builds every href
// through net/url, replacing the sort parameters rather than
// substituting into a caller's pattern string.
//
// Cells are pre-rendered HTML so callers control formatting.

// Column describes one DataTable column.
type Column struct {
	// Key is the column identifier used for sort state and matching
	// against Row.Cells. Required.
	Key string

	// Header is the visible column header text. May be empty: an
	// actions or icon column. A sortable column with no header gets
	// its sort anchor named from the column Key.
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
	// which the primitive renders for every headered column.
	ResponsiveCards ResponsiveMode = "cards"
)

// DataTableConfig configures a DataTable.
//
// Note there are two pagination packages, and DataTable uses exactly
// one of them: the Pagination field takes a
// core-ui/patterns/pagination.Config, which renders the page-link nav
// below the table. The other package, framework/pagination, is the
// server side of the story. It parses ?limit/?offset/?cursor query
// params and builds cursor tokens for the auto-generated CRUD list
// endpoints, and never renders HTML. A typical handler uses
// framework/pagination to slice the data, then feeds the resulting
// page count into this config's Pagination nav.
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
	//   <div data-fui-signal="<Signal>" data-fui-signal-mode="html">
	//     {DataTable(...)}
	//   </div>
	Island headless.Island

	// Pagination is an optional pagination.Config. When set, the
	// pagination nav renders below the table, outside the scroll
	// region.
	Pagination *pagination.Config

	// Empty is the EmptyState shown when len(Rows) == 0, under the
	// table's head: an empty result still has named columns and
	// usable sort controls. If zero, a default empty state renders.
	Empty EmptyStateConfig

	// Responsive selects how the table behaves when its container is
	// narrow. Default keeps horizontal scroll; ResponsiveCards
	// collapses rows into labeled cards via container queries.
	Responsive ResponsiveMode

	// Ctx carries the per-request context used to resolve i18n
	// strings (empty-state labels, sort aria-labels, pagination
	// labels). When nil, English fallbacks are returned.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the list's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style, data-fui-* and the data-hui-* hooks.
	ExtraAttrs html.Attrs
}

// dataTableClasses dresses headless.Table's parts in this package's
// own vocabulary — the names the registered ui-data-table sheet
// matches. Only the parts a selector reads: the sheet reaches the
// head, rows and cells by tag inside .ui-data-table__table, so those
// parts carry no class. Alignment travels as the column variant: the
// same is-align-* class names the header and the cell variant.
var dataTableClasses = headless.Classes{
	headless.PartRoot:    "ui-data-table",
	headless.PartScroll:  "ui-data-table__scroll",
	headless.PartTable:   "ui-data-table__table",
	headless.PartCaption: "ui-data-table__caption",
	headless.PartSort:    "ui-data-table__sort",

	"header--center": "is-align-center",
	"header--end":    "is-align-end",
	"cell--center":   "is-align-center",
	"cell--end":      "is-align-end",
}

// DataTable renders the table: the headless primitive's structure,
// roles and sort anchors under this package's class map, the styled
// EmptyState in the primitive's empty slot, and the core-ui pager in
// a footer div of its own outside the scroll region.
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
		cols[i] = headless.Column{Key: c.Key, Header: c.Header, Sortable: c.Sortable, Variant: variant}
	}
	rows := make([]headless.Row, len(cfg.Rows))
	for i, r := range cfg.Rows {
		rows[i] = headless.Row{ID: r.ID, Cells: r.Cells}
	}
	query := scrubQueryValues(cfg.Query)

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
		if cfg.Island.Endpoint != "" && pag.IslandSignal == "" {
			pag.IslandSignal = cfg.Island.Signal
			pag.IslandEndpoint = cfg.Island.Endpoint
		}
		// Thread i18n labels into the pagination nav (Previous / Next /
		// "Pagination") when the caller hasn't set them explicitly. The
		// pagination package is in core-ui (which may not import i18nui),
		// so the labels are resolved here and passed via the config.
		if pag.Label == "" {
			pag.Label = i18nui.T(ctx, i18nui.KeyPaginationLabel)
		}
		if pag.PrevLabel == "" {
			pag.PrevLabel = i18nui.T(ctx, i18nui.KeyPaginationPrevious)
		}
		if pag.NextLabel == "" {
			pag.NextLabel = i18nui.T(ctx, i18nui.KeyPaginationNext)
		}
		footer = html.Div(html.DivConfig{Class: "ui-data-table__footer"}, pagination.New(pag))
	}

	// The root's modifier classes travel as part attrs, which append
	// to the class map's own root class rather than replacing it.
	rootClass := cfg.Class
	if cfg.Responsive == ResponsiveCards {
		rootClass = "ui-data-table--responsive-cards " + rootClass
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
		// the caption's own class (ui-data-table__caption stays, the
		// visually-hidden recipe takes it out of the paint) while the
		// element, its id and its text stay for aria-labelledby.
		if parts.Attrs == nil {
			parts.Attrs = headless.PartAttrs{}
		}
		parts.Attrs[headless.PartCaption] = html.Attrs{"class": "ui-visually-hidden"}
	}

	return dataTableStyle.WrapHTML(headless.Table(headless.TableProps{
		Columns:    cols,
		Rows:       rows,
		Caption:    cfg.Caption,
		SortBy:     cfg.SortBy,
		SortDir:    headless.SortDir(cfg.SortDir),
		Path:       cfg.Path,
		Query:      query,
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

// scrubQueryValues strips C0 control bytes and DEL from the carried
// query's values. The query a screen carries is request state, and a
// hostile ?q= with CR/LF percent-encodes to %0D/%0A in the href —
// which the anchor policy refuses for every URL this framework writes,
// and the primitive (whose props are typed configuration) refuses at
// render. This component is the request boundary, so the scrub happens
// here, once, for every caller: a control byte in a carried value is
// never a search a user meant, and a pair whose value scrubs to
// nothing is dropped rather than carried as an empty filter.
func scrubQueryValues(q url.Values) url.Values {
	if len(q) == 0 {
		return q
	}
	out := make(url.Values, len(q))
	for k, vs := range q {
		cleaned := make([]string, 0, len(vs))
		for _, v := range vs {
			if s := scrubControlBytes(v); s != "" {
				cleaned = append(cleaned, s)
			}
		}
		if len(cleaned) > 0 {
			out[k] = cleaned
		}
	}
	return out
}

// scrubControlBytes removes every C0 control byte and DEL from s.
func scrubControlBytes(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
