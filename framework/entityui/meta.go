package entityui

import (
	"context"
	"fmt"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// meta is one entity's screen metadata, resolved per render: the entity,
// its Display (zero value when nil), its visible fields, its CRUD handle
// and REST base, and the app's translator.
type meta struct {
	e      *entity.Entity
	name   string
	d      entity.DisplayConfig
	states *entity.StatesConfig
	fields []schema.Field // every field the API returns: not Hidden
	byName map[string]schema.Field
	pk     string
	ch     *crud.CrudHandler
	api    string
	hasAPI bool
	tr     *i18n.Translator
	ext    Extension
}

func (u *UI) meta(name string) (*meta, error) {
	e, err := u.entityFor(name)
	if err != nil {
		return nil, err
	}
	ch, err := u.host.Crud(e)
	if err != nil {
		return nil, fmt.Errorf("entityui: entity %q: %w", name, err)
	}
	m := &meta{e: e, name: e.GetName(), states: e.Config.States, ch: ch, tr: u.host.Translator(), ext: u.ext.Entities[e.GetName()]}
	if e.Config.Display != nil {
		m.d = *e.Config.Display
	}
	m.api, m.hasAPI = u.host.APIPath(e)
	m.byName = map[string]schema.Field{}
	for _, f := range e.GetFields() {
		if f.Hidden {
			continue
		}
		m.fields = append(m.fields, f)
		m.byName[f.Name] = f
	}
	m.pk = e.PrimaryKey
	if m.pk == "" {
		m.pk = "id"
	}
	return m, nil
}

// field reports a visible field by name.
func (m *meta) field(name string) (schema.Field, bool) {
	f, ok := m.byName[name]
	return f, ok
}

func (m *meta) hint(field string) entity.FieldDisplay { return m.d.Fields[field] }

func (m *meta) singular(ctx context.Context) string {
	return i18nui.EntitySingular(ctx, m.tr, m.name, m.d.Singular)
}

func (m *meta) plural(ctx context.Context) string {
	return i18nui.EntityPlural(ctx, m.tr, m.name, m.d.Plural)
}

// noun is the entity's name inside a sentence: "11 customers".
func (m *meta) noun(ctx context.Context, plural bool) string {
	display := m.d.Singular
	if plural {
		display = m.d.Plural
	}
	return i18nui.EntityNoun(ctx, m.tr, m.name, display, plural)
}

func (m *meta) description(ctx context.Context) string {
	return i18nui.EntityDescription(ctx, m.tr, m.name, m.d.Description)
}

func (m *meta) label(ctx context.Context, field string) string {
	if f, ok := m.byName[field]; ok && f.Type == schema.Relation {
		return i18nui.RelationLabel(ctx, m.tr, m.name, field, m.hint(field).Label)
	}
	return i18nui.FieldLabel(ctx, m.tr, m.name, field, m.hint(field).Label)
}

func (m *meta) help(ctx context.Context, field string) string {
	return i18nui.FieldHelp(ctx, m.tr, m.name, field, m.hint(field).Help)
}

func (m *meta) valueLabel(ctx context.Context, field, value string) string {
	return i18nui.FieldValueLabel(ctx, m.tr, m.name, field, value)
}

// system reports the fields the server owns outright: the primary key,
// auto-generated fields and ReadOnly fields. The record header shows them;
// forms never carry them.
func (m *meta) system(f schema.Field) bool {
	return f.Name == m.pk || f.AutoGenerate != schema.AutoNone || f.ReadOnly
}

// guarded is the state field and every stamp of enforced or advisory
// States: drawn read-only on every screen, changed only by a move.
func (m *meta) guarded(field string) bool {
	if m.states == nil {
		return false
	}
	return slices.Contains(m.states.Guarded(), field)
}

// locked reports a field drawn read-only on forms and never submitted:
// Locked, guarded, or system.
func (m *meta) locked(f schema.Field) bool {
	return m.hint(f.Name).Locked || m.guarded(f.Name) || m.system(f)
}

// omitted reports a field left out of forms and columns on purpose.
func (m *meta) omitted(field string) bool { return m.hint(field).Omit }

// titleField is the field naming a record: Display.TitleField, else a
// visible "name" or "title" field, else the first String column that is
// not system, omitted or NoQuery (an invoice's number), else "" (the
// singular names it).
func (m *meta) titleField() string {
	if m.d.TitleField != "" {
		return m.d.TitleField
	}
	for _, n := range []string{"name", "title"} {
		if _, ok := m.byName[n]; ok {
			return n
		}
	}
	for _, f := range m.fields {
		if f.Type == schema.String && !f.NoQuery && !m.system(f) && !m.omitted(f.Name) {
			return f.Name
		}
	}
	return ""
}

// recordTitle names one row for headings, breadcrumbs and links.
func (m *meta) recordTitle(ctx context.Context, row map[string]any) string {
	if tf := m.titleField(); tf != "" {
		if s := cell(rowValue(row, tf)); s != "" {
			return s
		}
	}
	return m.singular(ctx)
}

// columns are the fields a list shows: Display.Columns, else every
// visible field that is not system, omitted, or JSON, in schema order.
func (m *meta) columns() []string {
	if len(m.d.Columns) > 0 {
		return slices.Clone(m.d.Columns)
	}
	var out []string
	for _, f := range m.fields {
		if m.system(f) || m.omitted(f.Name) || f.Type == schema.JSON {
			continue
		}
		out = append(out, f.Name)
	}
	return out
}

// rowValue reads a row value by declaration name, falling back to the
// camelCase spelling the JSON API serializes under camel casing.
func rowValue(row map[string]any, key string) any {
	if v, ok := row[key]; ok {
		return v
	}
	return row[casing.ToCamel(key)]
}
