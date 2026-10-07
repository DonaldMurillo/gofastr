package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ListBuilder draws one entity's list. Anything it does not set comes from
// the entity's Display. It is a component: return it from a screen, or
// call RenderCtx to place it inside another component.
type ListBuilder struct {
	component.ContextOnly
	ui     *UI
	entity string

	key      string
	columns  []string
	view     string
	as       string
	pageSize int
	where    []listWhere
	base     string
	heading  string
	level    int
	empty    string
	create   bool
	delete   bool
	dup      bool
	bulk     bool
	noLinks  bool
	queryBox bool
	colsMenu bool
	deleted  bool
	saved    bool
	counts   bool
	embedded bool
	top      bool
	actions  []render.HTML
}

type listWhere struct {
	field string
	value string
}

// List starts a list of the named entity. The name is checked when the
// list renders.
func (u *UI) List(entity string) *ListBuilder {
	return &ListBuilder{ui: u, entity: entity, create: true}
}

// Key namespaces the list's query params (<key>_sort, <key>_page,
// <key>_q, <key>_filter, <key>_view), so two lists on one page keep their
// own state. A page with two lists needs a key on each; the key follows
// the key grammar.
func (b *ListBuilder) Key(key string) *ListBuilder { b.key = key; return b }

// Columns replaces Display.Columns on this list.
func (b *ListBuilder) Columns(fields ...string) *ListBuilder { b.columns = fields; return b }

// View opens the list on a declared view when the URL names none.
func (b *ListBuilder) View(key string) *ListBuilder { b.view = key; return b }

// As draws rows as "table" or "cards".
func (b *ListBuilder) As(presentation string) *ListBuilder { b.as = presentation; return b }

// PageSize sets rows per page, capped by Pagination.MaxListLimit.
func (b *ListBuilder) PageSize(n int) *ListBuilder { b.pageSize = n; return b }

// Where pins field = value on every read of this list, ANDed inside the
// caller's scope (a tab listing one invoice's payments). The field must be
// queryable; value is matched as the field's type. A pinned field leaves
// the default columns and the facets, and New carries it as
// ?prefill_<field>=<value>.
func (b *ListBuilder) Where(field, value string) *ListBuilder {
	b.where = append(b.where, listWhere{field: field, value: value})
	return b
}

// Base is the path record links hang off (<base>/<id>, <base>/create).
// The default is the current request's path.
func (b *ListBuilder) Base(path string) *ListBuilder { b.base = path; return b }

// Heading replaces the list's heading text; level is 1 to 5 (0 means 1).
// The empty state takes the level below.
func (b *ListBuilder) Heading(text string, level int) *ListBuilder {
	b.heading, b.level = text, level
	return b
}

// Empty replaces the empty state's description.
func (b *ListBuilder) Empty(text string) *ListBuilder { b.empty = text; return b }

// NoCreate hides the New button even where the caller may create.
func (b *ListBuilder) NoCreate() *ListBuilder { b.create = false; return b }

// Delete and Duplicate turn on the row menu's delete and duplicate items,
// off by default on app pages. Each still needs the entity's access.
func (b *ListBuilder) Delete() *ListBuilder    { b.delete = true; return b }
func (b *ListBuilder) Duplicate() *ListBuilder { b.dup = true; return b }

// NoLinks draws the rows with no record links: the title is text, there
// is no row menu and no New. It is for an entity with no screen of its
// own on this app, such as a Related list of payments on an invoice.
func (b *ListBuilder) NoLinks() *ListBuilder { b.noLinks = true; return b }

// QueryBox turns on the filter's text box: a control named the list's
// filter param, prefilled with the active filter text and labelled with
// the entity's queryable field names, riding the list's GET form so a
// typed filter narrows the same way the chips do. Off by default on
// app pages; the admin turns it on.
func (b *ListBuilder) QueryBox() *ListBuilder { b.queryBox = true; return b }

// ColumnsMenu turns on the columns control: a disclosure holding a GET
// form that shows, hides, reorders and resets the list's columns
// through the cols param. A cols value naming anything but a visible,
// non-omitted field is ignored, never an error page; the title column
// stays — it carries the record link. Off by default on app pages.
func (b *ListBuilder) ColumnsMenu() *ListBuilder { b.colsMenu = true; return b }

// Deleted turns on the trash view beside the list's views, for an
// entity with Scope.SoftDelete (a no-op otherwise): ?view=deleted
// lists only soft-deleted rows under the same owner, tenant and read
// scope as the live list, and each row offers Restore and Delete
// permanently through the host's restore and purge handlers. Off by
// default on app pages.
func (b *ListBuilder) Deleted() *ListBuilder { b.deleted = true; return b }

// SavedViews turns on named saved views for this list, when the UI
// carries a SavedViewStore (UI.WithSavedViews; without one it is a
// no-op): the caller's views open through ?saved=, and a small form
// saves the active filter and columns under a name. Off by default on
// app pages.
func (b *ListBuilder) SavedViews() *ListBuilder { b.saved = true; return b }

// TabCounts shows each view tab's row count: the rows that tab's link
// lists, under the page's search, facets, filter, Where pins and read
// scope. The header then carries the entity's description, when it has
// one, in place of the count. One COUNT per tab per render; a refused
// count leaves its tab bare. Off by default on app pages; the admin
// turns it on.
func (b *ListBuilder) TabCounts() *ListBuilder { b.counts = true; return b }

// Embedded draws the list as one section of another screen, the way a
// record's Related tab shows its lists: a compact header holding the
// heading, the row count and a small "Add <singular>" button, then the
// rows with no view tabs, search or filters, and a one-line empty
// state when there are none. Sort and pager stay, keyed as ever.
func (b *ListBuilder) Embedded() *ListBuilder { b.embedded = true; return b }

// Top previews the list: its first n rows in the view's own order, with
// no pager, no sort controls and no row menu (each row still links to
// its record), and the URL's page and sort params ignored. It is for a list whose full form lives on another screen,
// such as a dashboard panel linking to it.
func (b *ListBuilder) Top(n int) *ListBuilder { b.pageSize = n; b.top = true; return b }

// mayCreate reports whether the list offers New: on unless NoCreate or
// NoLinks turned it off.
func (b *ListBuilder) mayCreate() bool { return b.create && !b.noLinks }

// Bulk turns on row selection and bulk actions, off by default on app
// pages and off for an entity with Display.NoBulk.
func (b *ListBuilder) Bulk() *ListBuilder { b.bulk = true; return b }

// Actions appends header actions beside New.
func (b *ListBuilder) Actions(a ...render.HTML) *ListBuilder {
	b.actions = append(b.actions, a...)
	return b
}

// RenderCtx draws the list for the request in ctx.
func (b *ListBuilder) RenderCtx(ctx context.Context) render.HTML {
	return contain(ctx, b.entity, "list", func() (render.HTML, error) {
		return b.render(ctx)
	})
}
