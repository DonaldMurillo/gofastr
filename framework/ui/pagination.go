package ui

import (
	"context"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// PaginationConfig configures the pager.
type PaginationConfig struct {
	// Page is the current page, 1-based. Must be within 1..Pages.
	Page int
	// Pages is the total number of pages. At least 1.
	Pages int

	// Path is the screen's path; empty makes relative "?query" hrefs.
	Path string
	// Query is the state every page href carries, PageParam replaced.
	Query url.Values
	// PageParam names the page query parameter. Empty defaults to "p".
	PageParam string
	// Window is the pages shown each side of the current one. Default 1.
	Window int
	// OmitPrevNext drops the Previous and Next anchors.
	OmitPrevNext bool
	// AriaLabel names the nav landmark; empty is the i18n "Pagination".
	AriaLabel string
	// PrevLabel and NextLabel override the end anchors' words.
	PrevLabel string
	NextLabel string
	// Island puts the GET RPC contract on the anchors, for a pager in an island.
	Island headless.Island
	// Ctx is the strings' language; nil is English.
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs land on the nav; class, id, style and the hooks are dropped.
	ExtraAttrs html.Attrs
}

var paginationClasses = headless.Classes{
	headless.PartPagination:    "pagination",
	headless.PartPaginationGap: "pagination-gap",
}

// Pagination renders the pager over a list's pages; Class lands on the list.
func Pagination(cfg PaginationConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.AriaLabel
	if label == "" {
		label = i18nui.PaginationLabel.Text(ctx)
	}
	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartPagination: {"class": cfg.Class}}
	}
	return paginationStyle.WrapHTML(headless.Pagination(headless.PaginationProps{
		Page:         cfg.Page,
		Pages:        cfg.Pages,
		Path:         cfg.Path,
		Query:        cfg.Query,
		PageParam:    cfg.PageParam,
		Window:       cfg.Window,
		OmitPrevNext: cfg.OmitPrevNext,
		AriaLabel:    label,
		PrevLabel:    cfg.PrevLabel,
		NextLabel:    cfg.NextLabel,
		Island:       cfg.Island,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:        parts,
		Strings:      StringsFor(ctx),
	}, paginationClasses))
}
