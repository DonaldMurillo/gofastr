package entityui

import (
	"context"
	"log/slog"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// RecordBuilder draws one record: header, move buttons, tabs (Edit,
// Related, extension tabs, and Activity and API where turned on) and
// the form from Display.Form. Create draws the same form empty.
type RecordBuilder struct {
	component.ContextOnly
	ui     *UI
	entity string
	id     string // "" for create

	base     string
	form     *entity.EntityForm
	omit     []string
	tabs     []Tab
	related  []relatedList
	activity bool
	apiTab   bool
	override bool
	delete   bool
	undo     bool
	dup      bool
	steps    bool
	prefill  map[string]string
}

// Record starts the record screen for one id.
func (u *UI) Record(entity, id string) *RecordBuilder {
	return &RecordBuilder{ui: u, entity: entity, id: id}
}

// Create starts the create screen: the record form, empty, posting a create.
func (u *UI) Create(entity string) *RecordBuilder {
	return &RecordBuilder{ui: u, entity: entity}
}

// RecordTitle names one record the way its record screen's heading
// does, for breadcrumbs and links drawn outside the screen. It reads
// behind the record's own gate (scope, sign-in, RBAC and a Decider's
// per-row answer), through the read hooks, and answers false for a
// record the caller may not see, a missing id and an unknown entity
// alike.
func (u *UI) RecordTitle(ctx context.Context, entityName, id string) (string, bool) {
	m, err := u.meta(entityName)
	if err != nil || !canReadRecord(ctx, m.ch, id) {
		return "", false
	}
	row, err := m.ch.GetOne(crud.WithReadHooks(ctx), id, nil)
	if err != nil || row == nil {
		return "", false
	}
	return u.recordTitle(ctx, m, row), true
}

// SnapshotTitle names a record from a stored copy of its values, an
// audit row's old or new side, the way RecordTitle names a live one: its
// title fields, else the entity's singular name. A masked title field
// is left out. It answers false for an entity this UI does not draw. It
// reads only the related records a relation title part names, so a
// deleted record still has a name.
func (u *UI) SnapshotTitle(ctx context.Context, entityName string, row map[string]any) (string, bool) {
	m, err := u.meta(entityName)
	if err != nil {
		return "", false
	}
	return u.recordTitle(ctx, m, row), true
}

// RecordMatch is one record SearchRecords found: its id and its title.
type RecordMatch struct {
	ID    string
	Title string
}

// maxRecordMatches caps one SearchRecords answer.
const maxRecordMatches = 20

// searchPages bounds how many pages of limit rows one SearchRecords
// reads looking past rows the caller may not open.
const searchPages = 5

// SearchRecords finds up to limit records (at most 20) whose
// SearchFields match q, the way the list's search box does, named as
// RecordTitle names them, in primary-key order. It reads behind the
// list's gate and each row's own, through the read hooks; the limit
// counts records the caller may open, and the read pages past refused
// rows for at most searchPages pages of limit rows. It answers nothing
// for an entity without SearchFields, an empty q, a caller who may not
// list the entity, an unknown entity and a failed read alike.
func (u *UI) SearchRecords(ctx context.Context, entityName, q string, limit int) []RecordMatch {
	q = strings.TrimSpace(q)
	m, err := u.meta(entityName)
	if err != nil || q == "" || len(m.e.Config.SearchFields) == 0 || !canRead(ctx, m.ch) {
		return nil
	}
	limit = min(max(limit, 1), maxRecordMatches)
	out := make([]RecordMatch, 0, limit)
	var found []map[string]any
	for page := range searchPages {
		rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
			Search: q, Limit: limit, Offset: page * limit, Sorts: []filter.ParsedSort{{Field: m.pk}},
		})
		if err != nil {
			slog.WarnContext(ctx, "entityui: search records", "entity", entityName, "error", err)
			return nil
		}
		for _, row := range rows {
			// A Decider may allow the list and refuse one row: a match
			// names only a record whose own screen would open.
			id := cell(rowValue(row, m.pk))
			if id == "" || !canReadRecord(ctx, m.ch, id) {
				continue
			}
			out = append(out, RecordMatch{ID: id})
			found = append(found, row)
			if len(out) == limit {
				break
			}
		}
		if len(out) == limit || len(rows) < limit {
			break
		}
	}
	// Named in one batch: a relation title part is one read for every
	// match, not one per match.
	for i, t := range u.rowTitles(ctx, m, found, 0) {
		if t == "" {
			t = m.singular(ctx)
		}
		out[i].Title = t
	}
	return out
}

// Base is the entity's list path on this app; Back, Cancel and the
// after-save navigation land there or on <base>/<id>. The default is the
// current request path with its last segment removed.
func (b *RecordBuilder) Base(path string) *RecordBuilder { b.base = path; return b }

// Form replaces Display.Form on this page.
func (b *RecordBuilder) Form(f *entity.EntityForm) *RecordBuilder { b.form = f; return b }

// Omit leaves fields out of this page's form and detail.
func (b *RecordBuilder) Omit(fields ...string) *RecordBuilder {
	b.omit = append(b.omit, fields...)
	return b
}

// Tab adds a tab on this page only, after the entity's extension tabs.
func (b *RecordBuilder) Tab(key string, build func(TabContext) (component.Component, error)) *RecordBuilder {
	b.tabs = append(b.tabs, Tab{Key: key, Build: build})
	return b
}

// Related names the entities whose lists the Related tab shows on this
// page. App pages show none unless named; the admin passes every entity
// it exposes. Each list still passes that entity's own read gate.
func (b *RecordBuilder) Related(entities ...string) *RecordBuilder {
	for _, name := range entities {
		b.related = append(b.related, relatedList{name: name})
	}
	return b
}

// RelatedAt names one related entity whose list hangs off base instead
// of the path Related derives. An empty base draws that list with no
// links and no New (ListBuilder.NoLinks): the entity has no screen of its
// own on this app.
func (b *RecordBuilder) RelatedAt(entity, base string) *RecordBuilder {
	b.related = append(b.related, relatedList{name: entity, base: base, fixed: true})
	return b
}

// relatedList is one Related tab list: the entity, and the base its
// record links hang off when fixed (RelatedAt) rather than derived.
type relatedList struct {
	name  string
	base  string
	fixed bool
}

// Activity turns on the Activity tab: this record's audit rows. Off by
// default on app pages, and never drawn when the app keeps no audit log.
func (b *RecordBuilder) Activity() *RecordBuilder { b.activity = true; return b }

// API turns on the API tab: the record as the JSON API's GET returns
// it, the entity's own REST path with the methods its exposure allows,
// its MCP tool names, and a link to the API's entity index. Off by
// default. The tab reads nothing the record screen could not.
func (b *RecordBuilder) API() *RecordBuilder { b.apiTab = true; return b }

// Override turns on the status override: a form, inside a disclosure,
// that writes the state field directly through crud.WithStateOverride
// for a caller holding <entity>:override_state exactly (a Wildcard
// grant and a back office's elevation never satisfy it). Off by
// default, and drawn only for an entity with enforced states on an app
// that keeps an audit log; UI.OverrideHandler serves its posts.
func (b *RecordBuilder) Override() *RecordBuilder { b.override = true; return b }

// Delete and Duplicate turn on those record actions.
func (b *RecordBuilder) Delete() *RecordBuilder    { b.delete = true; return b }
func (b *RecordBuilder) Duplicate() *RecordBuilder { b.dup = true; return b }

// Undo puts Undo on the toast a Delete leaves, for an entity with
// Scope.SoftDelete (a no-op otherwise) and a caller who may update the
// record: it restores the record through the host's RestoreHandler and
// returns to the list. Off by default.
func (b *RecordBuilder) Undo() *RecordBuilder { b.undo = true; return b }

// Prefill sets starting values on a create form (a foreign key when New
// is opened from a related list). Fields a create may not set are ignored.
func (b *RecordBuilder) Prefill(values map[string]string) *RecordBuilder {
	b.prefill = values
	return b
}

// RenderCtx draws the record or create screen for the request in ctx.
func (b *RecordBuilder) RenderCtx(ctx context.Context) render.HTML {
	return contain(ctx, b.entity, "record", func() (render.HTML, error) {
		return b.render(ctx)
	})
}
