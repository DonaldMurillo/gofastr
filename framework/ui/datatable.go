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

// Column describes one DataTable column.
type Column struct {
	// Key identifies the column in sort state and Row.Cells. Required.
	Key string
	// Header is the header text; empty is allowed, and a sortable
	// column then names its anchor from Key.
	Header string
	// Sortable makes the header an anchor to the next sort direction.
	Sortable bool
	// Align is "start" (default), "center" or "end".
	Align string
	// Wrap lets the cells wrap; other cells hold one line. Excludes Fit.
	Wrap bool
	// Fit narrows the column to its content. Excludes Wrap and Truncate.
	Fit bool
	// Truncate cuts a long value to one capped line. Excludes Wrap.
	Truncate bool
	// SelectAll makes the header a checkbox over the row checkboxes of
	// that name; an empty table draws none.
	SelectAll string
	// Phone places the cells in a ResponsiveRows phone row.
	Phone PhoneSlot
}

// Row is a single rendered table row. Cells map column Key → HTML.
type Row struct {
	// Cells maps column Key to cell HTML; a missing cell renders empty.
	Cells map[string]render.HTML
	// ID is the row's optional id.
	ID string
}

// SortDir is the direction of a sort.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

// ResponsiveMode selects how a DataTable responds to a narrow
// container (a container query, not the viewport).
type ResponsiveMode string

const (
	// ResponsiveScroll keeps the table and scrolls it sideways.
	ResponsiveScroll ResponsiveMode = ""
	// ResponsiveCards draws each row as a stack of label/value lines.
	ResponsiveCards ResponsiveMode = "cards"
	// ResponsiveRows draws each row as a compact two-line phone row
	// from the columns' Phone slots; one column must be PhoneTitle.
	ResponsiveRows ResponsiveMode = "rows"
)

// PhoneSlot places a column's cells in a ResponsiveRows phone row; the
// zero value leaves the column off it.
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
type DataTableConfig struct {
	// Columns are the column definitions. Required.
	Columns []Column
	// Rows are the current page's rows.
	Rows []Row
	// Caption names the table and its scroll region.
	Caption string
	// CaptionHidden keeps Caption for assistive technology but hides
	// it from sight. Requires Caption.
	CaptionHidden bool
	// SortBy is the active sort column's Key; empty sorts none.
	SortBy  string
	SortDir SortDir
	// Summary describes the caller's result window ("1–25 of 40"); it
	// shows under the rows and joins the sort announcement.
	Summary string
	// Path is the base of each sort href; empty yields "?query".
	Path string
	// Query is the request state sort hrefs carry, with SortParam and
	// DirParam replaced.
	Query url.Values
	// SortParam and DirParam default to "sort" and "dir".
	SortParam string
	DirParam  string
	// Island puts the GET RPC contract on the sort anchors and is
	// inherited by Pagination; the caller wraps the table in the
	// signal-bound region.
	Island headless.Island
	// Pagination renders a pager below the table, outside the scroll
	// region.
	Pagination *PaginationConfig
	// Empty is the EmptyState drawn under the head when there are no
	// Rows; zero draws the default one.
	Empty EmptyStateConfig
	// Responsive selects the narrow-container behaviour.
	Responsive ResponsiveMode
	// Flush drops the table's frame, for rows inside a card.
	Flush bool
	// FooterTools sit under the rows, before the pager.
	FooterTools render.HTML
	// Note is a line of small print under the rows.
	Note string
	// Ctx resolves the table's strings; nil uses English.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs land on the root; class, id, style, data-cui-* and
	// data-hui-* are the component's.
	ExtraAttrs html.Attrs
}

var dataTableClasses = headless.Classes{
	headless.PartRoot:        "fui-data-table",
	headless.PartScroll:      "fui-data-table__scroll",
	headless.PartTable:       "fui-data-table__table",
	headless.PartCaption:     "fui-data-table__caption",
	headless.PartSort:        "fui-data-table__sort",
	headless.PartEmpty:       "fui-data-table__empty",
	headless.PartStatus:      "fui-visually-hidden",
	headless.PartTableSelect: "fui-choice--checkbox fui-data-table__select",
	headless.PartControl:     "fui-choice__input",

	"header--fit":             "is-fit",
	"header--center-fit":      "is-align-center is-fit",
	"header--end-fit":         "is-align-end is-fit",
	"cell--fit":               "is-fit",
	"cell--center-fit":        "is-align-center is-fit",
	"cell--end-fit":           "is-align-end is-fit",
	"header--center":          "is-align-center",
	"header--end":             "is-align-end",
	"header--center-wrap":     "is-align-center",
	"header--end-wrap":        "is-align-end",
	"cell--center":            "is-align-center",
	"cell--end":               "is-align-end",
	"cell--wrap":              "is-wrap",
	"cell--center-wrap":       "is-align-center is-wrap",
	"cell--end-wrap":          "is-align-end is-wrap",
	"cell--truncate":          "is-truncate",
	"header--center-truncate": "is-align-center",
	"cell--center-truncate":   "is-align-center is-truncate",
	"header--end-truncate":    "is-align-end",
	"cell--end-truncate":      "is-align-end is-truncate",
}

var phoneSlots = []PhoneSlot{PhoneLead, PhoneTitle, PhoneSubtitle, PhoneMeta, PhoneDetail, PhoneEnd}

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

// DataTable renders headless.Table under this package's class map, with
// EmptyState in its empty slot and Pagination in a footer outside the
// scroll region. It panics on a configuration that cannot render.
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
			e.Title = i18nui.TableNoResults.Text(ctx)
			if e.Description == "" {
				e.Description = i18nui.TableEmptyDesc.Text(ctx)
			}
		}
		empty = EmptyState(e)
	}

	var footer, pager render.HTML
	if cfg.Pagination != nil {
		pag := *cfg.Pagination
		if pag.Island.Endpoint == "" && pag.Island.Signal == "" {
			pag.Island = cfg.Island
		}
		if pag.Ctx == nil {
			pag.Ctx = ctx
		}
		pager = Pagination(pag)
	}
	if pager != "" || cfg.Summary != "" || cfg.FooterTools != "" {
		var summary render.HTML
		if cfg.Summary != "" {
			summary = html.Span(html.TextConfig{Class: "fui-data-table__summary", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(cfg.Summary))
		}
		attrs := html.Attrs{}
		if cfg.FooterTools == "" {
			attrs["data-cui-internal"] = ""
		} else if pager != "" {
			pager = headless.Own(pager)
		}
		footer = html.Div(html.DivConfig{Class: "fui-data-table__footer", ExtraAttrs: attrs},
			summary, cfg.FooterTools, pager)
	}
	if cfg.Note != "" {
		footer = render.Join(footer, render.Tag("p", map[string]string{"class": "fui-data-table__note", "data-cui-internal": ""}, render.Text(cfg.Note)))
	}

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
