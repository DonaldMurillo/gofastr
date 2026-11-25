package entityui

import (
	"context"
	"log/slog"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
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
	steps    *ListBuilder
	panel    bool
	prefill  map[string]string
}

// Record starts the record screen for one id.
func (u *UI) Record(entity, id string) *RecordBuilder {
	return &RecordBuilder{ui: u, entity: entity, id: id}
}

// Create starts the create screen: the record form, empty, posting a create.
// Mount it at <base>/create, or configure UI.WithCreatePath with its route.
func (u *UI) Create(entity string) *RecordBuilder {
	return &RecordBuilder{ui: u, entity: entity}
}

// RecordTitle names one record the way its record screen's heading
// does, read behind the record's own gate and the read hooks. It answers
// false for a record the caller may not see, a missing id and an unknown
// entity alike.
func (u *UI) RecordTitle(ctx context.Context, entityName, id string) (string, bool) {
	m, err := u.meta(entityName)
	if err != nil {
		return "", false
	}
	row, err := m.readableRow(ctx, id)
	if err != nil || row == nil {
		return "", false
	}
	return u.recordTitle(ctx, m, row), true
}

func (m *meta) readableRow(ctx context.Context, id string) (map[string]any, error) {
	if id == "" || !canReadRecord(ctx, m.ch, id) {
		return nil, nil
	}
	row, err := m.getRow(ctx, id)
	if crud.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// lookupRecord returns the row a search or picker may show for id, which
// the list read returned as listed: the record as the record screen
// reads it, or nil when the caller may not see it. A failed read or hook
// error hides that record only. With no get hooks the listed row is the
// record, so a lookup costs no read per row.
func (m *meta) lookupRecord(ctx context.Context, id string, listed map[string]any) map[string]any {
	if m.ch.Hooks == nil || len(m.ch.Hooks.HooksFor(hook.BeforeGet))+len(m.ch.Hooks.HooksFor(hook.AfterGet)) == 0 {
		if id == "" || !canReadRecord(ctx, m.ch, id) {
			return nil
		}
		return listed
	}
	row, err := m.readableRow(ctx, id)
	if err != nil {
		slog.WarnContext(ctx, "entityui: lookup record hidden", "entity", m.name, "error", err)
		return nil
	}
	return row
}

// RecordHidden reports whether the caller's read hooks keep record id
// out of sight; a screen that draws a record from a stored copy of its
// values asks it first. A failed read or hook error hides the record. An
// entity with no Before hooks, and an unknown entity, hide nothing.
func (u *UI) RecordHidden(ctx context.Context, entityName, id string) bool {
	m, err := u.meta(entityName)
	if err != nil || m.ch.Hooks == nil ||
		len(m.ch.Hooks.HooksFor(hook.BeforeGet))+len(m.ch.Hooks.HooksFor(hook.BeforeList)) == 0 {
		return false
	}
	row, err := m.getRowUnhooked(ctx, id)
	switch {
	case err == nil && row != nil:
		hooked, err := m.getRow(ctx, id)
		return err != nil || hooked == nil
	case err != nil && !crud.IsNotFound(err):
		return true
	}
	if m.e.Config.Scope != nil && m.e.Config.Scope.SoftDelete {
		opts := crud.ListOptions{Fields: m.keys, Deleted: true}
		raw, err := m.listByIDsUnhooked(ctx, []string{id}, opts)
		if err != nil {
			return true
		}
		if len(raw) > 0 {
			hooked, err := m.listByIDs(ctx, []string{id}, opts)
			return err != nil || len(hooked) == 0
		}
	}
	return m.ch.ReadHooksRestrict(ctx, id)
}

// SnapshotTitle names a record from a stored copy of its values (an
// audit row's side) the way RecordTitle names a live one, masked title
// fields left out. It answers false for an entity this UI does not draw.
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

const maxRecordMatches = 20

const searchPages = 5

// SearchRecords finds up to limit records (at most 20) the caller may
// open whose SearchFields match q, titled as RecordTitle titles them, in
// primary-key order. It answers nothing for an entity without
// SearchFields, an empty q, a caller who may not list the entity, an
// unknown entity and a failed read alike.
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
		rows, err := m.listRows(ctx, crud.ListOptions{
			Search: q, Limit: limit, Offset: page * limit, Sorts: m.keySorts(),
		})
		if err != nil {
			slog.WarnContext(ctx, "entityui: search records", "entity", entityName, "error", err)
			return nil
		}
		for _, row := range rows {
			id := m.rowID(row)
			record := m.lookupRecord(ctx, id, row)
			if record == nil {
				continue
			}
			out = append(out, RecordMatch{ID: id})
			found = append(found, record)
			if len(out) == limit {
				break
			}
		}
		if len(out) == limit || len(rows) < limit {
			break
		}
	}
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
// links and no New.
func (b *RecordBuilder) RelatedAt(entity, base string) *RecordBuilder {
	b.related = append(b.related, relatedList{name: entity, base: base, fixed: true})
	return b
}

type relatedList struct {
	name  string
	base  string
	fixed bool
}

// Activity turns on the Activity tab: this record's audit rows. Off by
// default on app pages, and never drawn when the app keeps no audit log.
func (b *RecordBuilder) Activity() *RecordBuilder { b.activity = true; return b }

// API turns on the API tab: the record as the JSON API's GET returns
// it, the entity's REST paths and methods, and its MCP tool names. Off
// by default.
func (b *RecordBuilder) API() *RecordBuilder { b.apiTab = true; return b }

// Override turns on the status override: a form that writes the state
// field directly for a caller holding <entity>:override_state exactly.
// Off by default; drawn only for an entity with enforced states on an app
// that keeps an audit log. UI.OverrideHandler serves its posts.
func (b *RecordBuilder) Override() *RecordBuilder { b.override = true; return b }

// Delete and Duplicate turn on those record actions.
func (b *RecordBuilder) Delete() *RecordBuilder    { b.delete = true; return b }
func (b *RecordBuilder) Duplicate() *RecordBuilder { b.dup = true; return b }

// Undo puts Undo on the toast a Delete leaves, for an entity with
// Scope.SoftDelete (a no-op otherwise) and a caller who may update the
// record. Off by default.
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
