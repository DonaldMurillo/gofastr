package ui

import (
	"context"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Pagination is the pager over a list of results: headless.Pagination's
// structure dressed with this package's class map and a registered
// sheet of its own. The class names are the ones the core-ui pattern's
// sheet reads — "pagination" on the list, "pagination-gap" on the gap —
// kept so a host stylesheet written against the pattern's names keeps
// matching; the anatomy is the headless one (nav > div > a|span, no
// ol/li, and no buttons: an island pager's anchors keep their hrefs),
// so the sheet selects anchors and gaps, not list items.
//
// The page change is typed props, not a pattern string: the caller
// gives the screen's Path, the query it carries (url.Values) and the
// page parameter's name, and the primitive builds every href through
// net/url with the page parameter replaced rather than appended — the
// request-derived-string defect class a "%d" pattern carries cannot be
// expressed here.

// PaginationConfig configures the pager.
type PaginationConfig struct {
	// Page is the current page, 1-based. Must be within 1..Pages.
	Page int
	// Pages is the total number of pages. At least 1.
	Pages int

	// Path is the screen's own path: each page href is it plus the
	// carried query, the page parameter replaced. Empty means the
	// current document — a relative "?query" href.
	Path string
	// Query is the request state the page anchors carry unchanged:
	// the search, the filters, the sort — anything a page turn must
	// not drop. The primitive replaces PageParam in it rather than
	// appending duplicate pairs.
	Query url.Values
	// PageParam names the page query parameter. Empty defaults to "p".
	PageParam string

	// Window is the number of pages shown each side of the current
	// one, the first and last always shown. Default 1; a Window large
	// enough shows every page.
	Window int
	// OmitPrevNext drops the Previous and Next anchors entirely.
	OmitPrevNext bool

	// AriaLabel names the nav landmark for AT. Empty resolves to the
	// reader's "Pagination" through the i18n keys.
	AriaLabel string
	// PrevLabel and NextLabel override the end anchors' words, which
	// otherwise resolve through the i18n keys.
	PrevLabel string
	NextLabel string

	// Island is optional. A zero value keeps plain page anchors —
	// the URL is the truth for a list, and a list screen's pager is
	// list state. A set value puts the GET RPC contract on those same
	// anchors, for a pager embedded in an island region (a
	// DataTable's footer under an island table): the href is still
	// the page without script, the region update with it.
	Island headless.Island

	// Ctx carries the per-request context used to resolve the i18n
	// strings (the nav label, the end anchors' words). When nil,
	// English fallbacks are returned.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the nav landmark. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// style, data-fui-* and the data-hui-* hooks.
	ExtraAttrs html.Attrs
}

// paginationClasses dresses headless.Pagination's parts in the names
// the pager's sheet matches. Only the parts a selector reads: the
// sheet reaches the anchors by tag inside .pagination, so the links
// carry no class, and the nav landmark itself carries none — the
// headless contract, and where the pattern's <ol> always carried the
// caller's Class instead.
var paginationClasses = headless.Classes{
	headless.PartPagination:    "pagination",
	headless.PartPaginationGap: "pagination-gap",
}

// Pagination renders the pager: the headless primitive's structure,
// roles and page anchors under this package's class map, wrapped for
// its sheet. A caller's Class lands on the list, where the pattern's
// Class always landed.
func Pagination(cfg PaginationConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.AriaLabel
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeyPaginationLabel)
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
