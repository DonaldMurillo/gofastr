package ui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/pagination"
	"github.com/gofastr/gofastr/core/render"
)

// DataTable is a server-rendered list view that composes core-ui's
// Table + Pagination with framework/ui's EmptyState. It is fully
// server-driven — sort and pagination are link-based, no JS — and is
// designed for entity-CRUD admin lists where rows are paginated server
// data.
//
// Cells are pre-rendered HTML so callers control formatting. Sort
// state is reflected in the URL via SortHrefPattern so it round-trips
// cleanly with browser history.

// Column describes one DataTable column.
type Column struct {
	// Key is the column identifier used for sort state and matching
	// against Row.Cells. Required.
	Key string

	// Header is the visible column header text. Required.
	Header string

	// Sortable enables a clickable sort link in the header.
	Sortable bool

	// Align is "start" (default), "center", or "end".
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

// DataTableConfig configures a DataTable.
type DataTableConfig struct {
	// Columns is the column definitions. Required.
	Columns []Column

	// Rows is the rendered rows for the current page.
	Rows []Row

	// Caption is an accessible table caption (optional).
	Caption string

	// SortBy is the active sort column's Key (optional).
	SortBy string

	// SortDir is the active sort direction (asc/desc).
	SortDir SortDir

	// SortHrefPattern is a Sprintf pattern with two %s placeholders
	// for column key and direction, e.g. "?sort=%s&dir=%s". Required
	// if any column is Sortable.
	SortHrefPattern string

	// Pagination is an optional pagination.Config. When set, the
	// pagination nav renders below the table.
	Pagination *pagination.Config

	// Empty is the EmptyState shown when len(Rows) == 0. If zero,
	// a default empty state is rendered.
	Empty EmptyStateConfig

	ID    string
	Class string
}

// DataTable renders the table.
func DataTable(cfg DataTableConfig) render.HTML {
	if len(cfg.Columns) == 0 {
		panic("ui: DataTable requires at least one Column")
	}
	for _, c := range cfg.Columns {
		if c.Key == "" {
			panic("ui: DataTable Column requires Key")
		}
		if c.Header == "" {
			panic("ui: DataTable Column requires Header")
		}
		if c.Sortable && cfg.SortHrefPattern == "" {
			panic("ui: DataTable Column.Sortable requires Config.SortHrefPattern")
		}
	}

	// Empty state — composed via the EmptyState semantic component.
	if len(cfg.Rows) == 0 {
		empty := cfg.Empty
		if empty.Title == "" {
			empty.Title = "No results"
			if empty.Description == "" {
				empty.Description = "Adjust your filters or add new entries."
			}
		}
		return elements.Div(elements.DivConfig{
			Class: wrapClass(cfg.Class, "ui-data-table is-empty"),
			ID:    cfg.ID,
		}, EmptyState(empty))
	}

	// Header — composed via elements.TR + elements.TH so ARIA scope
	// and column-header semantics come from core-ui.
	thCells := make([]render.HTML, len(cfg.Columns))
	for i, col := range cfg.Columns {
		thCells[i] = renderHeader(col, cfg.SortBy, cfg.SortDir, cfg.SortHrefPattern)
	}
	thead := elements.Thead(elements.TableSectionConfig{},
		elements.TableRow(elements.TableRowConfig{}, thCells...),
	)

	// Body.
	bodyRows := make([]render.HTML, len(cfg.Rows))
	for i, r := range cfg.Rows {
		cells := make([]render.HTML, len(cfg.Columns))
		for j, col := range cfg.Columns {
			content, ok := r.Cells[col.Key]
			if !ok {
				content = render.Text("")
			}
			cellCfg := elements.TDConfig{}
			if col.Align != "" && col.Align != "start" {
				cellCfg.Class = "is-align-" + col.Align
			}
			cells[j] = elements.TD(cellCfg, content)
		}
		bodyRows[i] = elements.TableRow(elements.TableRowConfig{ID: r.ID}, cells...)
	}
	tbody := elements.Tbody(elements.TableSectionConfig{}, bodyRows...)

	// Caption (when set) goes inside the table; elements.Caption
	// wraps it in a proper <caption>.
	tableChildren := []render.HTML{}
	if cfg.Caption != "" {
		tableChildren = append(tableChildren,
			elements.Caption(elements.CaptionConfig{Class: "ui-data-table__caption"},
				render.Text(cfg.Caption)))
	}
	tableChildren = append(tableChildren, thead, tbody)
	table := elements.Table(
		elements.TableConfig{Class: "ui-data-table__table"},
		tableChildren...)

	children := []render.HTML{
		elements.Div(elements.DivConfig{Class: "ui-data-table__scroll"}, table),
	}
	if cfg.Pagination != nil {
		children = append(children,
			elements.Div(elements.DivConfig{Class: "ui-data-table__footer"},
				pagination.New(*cfg.Pagination)))
	}
	return elements.Div(elements.DivConfig{
		Class: wrapClass(cfg.Class, "ui-data-table"), ID: cfg.ID,
	}, children...)
}

// wrapClass concatenates a base class with optional caller-supplied
// classes, producing "base extra1 extra2" or just "base".
func wrapClass(extra, base string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}

func renderHeader(col Column, activeKey string, activeDir SortDir, pattern string) render.HTML {
	thCfg := elements.THConfig{Scope: "col"}
	if col.Align != "" && col.Align != "start" {
		thCfg.Class = "is-align-" + col.Align
	}
	thAttrs := elements.Attrs{}
	if !col.Sortable {
		thCfg.Attrs = thAttrs
		return elements.TH(thCfg, render.Text(col.Header))
	}

	// Compute next sort: clicking the active column flips direction;
	// otherwise default to asc.
	nextDir := SortAsc
	indicator := ""
	if col.Key == activeKey {
		if activeDir == SortAsc {
			nextDir = SortDesc
			indicator = "↑"
		} else {
			nextDir = SortAsc
			indicator = "↓"
		}
		thAttrs["aria-sort"] = string(activeDir) + "ending"
	} else {
		thAttrs["aria-sort"] = "none"
	}
	thCfg.Attrs = thAttrs

	href := fmt.Sprintf(pattern, url.QueryEscape(col.Key), url.QueryEscape(string(nextDir)))
	link := elements.LinkHTML(elements.LinkHTMLConfig{
		Href:  href,
		Class: "ui-data-table__sort",
		Content: render.Join(
			render.Text(col.Header),
			elements.Span(elements.TextConfig{
				Class: "ui-data-table__sort-indicator",
				Attrs: elements.Attrs{"aria-hidden": "true"},
			}, render.Text(strings.TrimSpace(indicator))),
		),
	})
	return elements.TH(thCfg, link)
}
