package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ListBuilder draws one entity's list; what it does not set comes from the
// entity's Display.
type ListBuilder struct {
	component.ContextOnly
	ui     *UI
	entity string

	key          string
	columns      []string
	view         string
	as           string
	pageSize     int
	where        []listWhere
	base         string
	heading      string
	level        int
	empty        string
	create       bool
	delete       bool
	undo         bool
	dup          bool
	bulk         bool
	noLinks      bool
	queryBox     bool
	colsMenu     bool
	deleted      bool
	saved        bool
	counts       bool
	embedded     bool
	flush        bool
	top          bool
	layoutSwitch bool
	inlineEdit   bool
	actions      []render.HTML
}

type listWhere struct {
	field string
	value string
}

// List starts a list of the named entity, checked when it renders.
func (u *UI) List(entity string) *ListBuilder {
	return &ListBuilder{ui: u, entity: entity, create: true}
}

// Key namespaces the list's query params (<key>_sort, <key>_page, ...); two
// lists on one page each need one.
func (b *ListBuilder) Key(key string) *ListBuilder { b.key = key; return b }

// Columns replaces Display.Columns on this list.
func (b *ListBuilder) Columns(fields ...string) *ListBuilder { b.columns = fields; return b }

// View opens the list on a declared view when the URL names none.
func (b *ListBuilder) View(key string) *ListBuilder { b.view = key; return b }

// As draws rows as "table" or "cards".
func (b *ListBuilder) As(presentation string) *ListBuilder { b.as = presentation; return b }

// InlineEdit edits a table's plain, unmasked cells in place for a caller
// who may update the row; saving PUTs the one field to its write route.
func (b *ListBuilder) InlineEdit() *ListBuilder { b.inlineEdit = true; return b }

// LayoutSwitch draws a Table / Cards switch whose as param wins over As
// and the view's layout.
func (b *ListBuilder) LayoutSwitch() *ListBuilder { b.layoutSwitch = true; return b }

// PageSize sets rows per page, capped by Pagination.MaxListLimit.
func (b *ListBuilder) PageSize(n int) *ListBuilder { b.pageSize = n; return b }

// Where pins field = value on every read of this list, ANDed inside the
// caller's scope; New carries it as ?prefill_<field>=<value>.
func (b *ListBuilder) Where(field, value string) *ListBuilder {
	b.where = append(b.where, listWhere{field: field, value: value})
	return b
}

// Base is the path record links hang off (<base>/<id>, <base>/create);
// the default is the request's path.
func (b *ListBuilder) Base(path string) *ListBuilder { b.base = path; return b }

// Heading replaces the list's heading text; level is 1 to 5 (0 means 1).
func (b *ListBuilder) Heading(text string, level int) *ListBuilder {
	b.heading, b.level = text, level
	return b
}

// Empty replaces the empty state's description.
func (b *ListBuilder) Empty(text string) *ListBuilder { b.empty = text; return b }

// NoCreate hides the New button even where the caller may create.
func (b *ListBuilder) NoCreate() *ListBuilder { b.create = false; return b }

// Delete and Duplicate turn on the row menu's delete and duplicate items.
func (b *ListBuilder) Delete() *ListBuilder    { b.delete = true; return b }
func (b *ListBuilder) Duplicate() *ListBuilder { b.dup = true; return b }

// Undo puts Undo on a row Delete's toast, for a Scope.SoftDelete entity and
// a caller who may update the row.
func (b *ListBuilder) Undo() *ListBuilder { b.undo = true; return b }

// NoLinks draws the rows with no record links and no row menu.
func (b *ListBuilder) NoLinks() *ListBuilder { b.noLinks = true; return b }

// QueryBox turns on the filter's text box on the list's GET form.
func (b *ListBuilder) QueryBox() *ListBuilder { b.queryBox = true; return b }

// ColumnsMenu turns on the columns control, a GET form over the cols param;
// a cols value naming no visible field is ignored.
func (b *ListBuilder) ColumnsMenu() *ListBuilder { b.colsMenu = true; return b }

// Deleted turns on the trash view (?view=deleted) for a Scope.SoftDelete
// entity, with Restore and Delete permanently on each row.
func (b *ListBuilder) Deleted() *ListBuilder { b.deleted = true; return b }

// SavedViews turns on named saved views (?saved=) when the UI carries a
// SavedViewStore.
func (b *ListBuilder) SavedViews() *ListBuilder { b.saved = true; return b }

// TabCounts shows each view tab's row count, one COUNT per tab per render;
// a refused count leaves its tab bare.
func (b *ListBuilder) TabCounts() *ListBuilder { b.counts = true; return b }

// Embedded draws the list as a section of another screen: a compact
// header and the rows, with no view tabs, search or filters.
func (b *ListBuilder) Embedded() *ListBuilder { b.embedded = true; return b }

// Flush draws the table without its frame.
func (b *ListBuilder) Flush() *ListBuilder { b.flush = true; return b }

// Top previews the list's first n rows in the view's order, with no pager,
// sort controls or row menu, ignoring the URL's page and sort params.
func (b *ListBuilder) Top(n int) *ListBuilder { b.pageSize = n; b.top = true; return b }

func (b *ListBuilder) mayCreate() bool { return b.create }

// Bulk turns on row selection and bulk actions, unless Display.NoBulk.
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
