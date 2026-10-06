package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// RecordBuilder draws one record: header, move buttons, tabs (Edit,
// Related, extension tabs, and Activity where turned on) and the form
// from Display.Form. Create draws the same form empty.
type RecordBuilder struct {
	component.ContextOnly
	ui     *UI
	entity string
	id     string // "" for create

	base     string
	form     *entity.EntityForm
	omit     []string
	tabs     []Tab
	related  []string
	activity bool
	delete   bool
	dup      bool
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
	b.related = append(b.related, entities...)
	return b
}

// Activity turns on the Activity tab: this record's audit rows. Off by
// default on app pages, and never drawn when the app keeps no audit log.
func (b *RecordBuilder) Activity() *RecordBuilder { b.activity = true; return b }

// Delete and Duplicate turn on those record actions.
func (b *RecordBuilder) Delete() *RecordBuilder    { b.delete = true; return b }
func (b *RecordBuilder) Duplicate() *RecordBuilder { b.dup = true; return b }

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
