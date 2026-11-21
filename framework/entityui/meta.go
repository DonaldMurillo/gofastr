package entityui

import (
	"context"
	"fmt"
	"net/url"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/internal/casing"
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
)

type meta struct {
	e      *entity.Entity
	name   string
	d      entity.DisplayConfig
	states *entity.StatesConfig
	fields []schema.Field
	byName map[string]schema.Field
	keys   []string
	pk     string
	wire   map[string]string
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
	m.keys = e.KeyColumns()
	m.pk, _ = e.KeyColumn()
	for _, f := range e.GetFields() {
		if f.WireName != "" && f.WireName != f.Name {
			if m.wire == nil {
				m.wire = map[string]string{}
			}
			m.wire[f.Name] = f.WireName
		}
	}
	return m, nil
}

func (m *meta) isKey(field string) bool { return slices.Contains(m.keys, field) }

func (m *meta) rowID(row map[string]any) string {
	parts := make([]string, len(m.keys))
	for i, c := range m.keys {
		if parts[i] = fieldtext.Cell(m.rowValue(row, c)); parts[i] == "" {
			return ""
		}
	}
	return entity.JoinKey(parts)
}

func (m *meta) idPath(id string) string {
	if len(m.keys) > 1 {
		if parts, err := entity.SplitKey(id, len(m.keys)); err == nil {
			return entity.JoinKey(parts)
		}
	}
	return url.PathEscape(id)
}

func (m *meta) keySorts() []filter.ParsedSort {
	out := make([]filter.ParsedSort, len(m.keys))
	for i, c := range m.keys {
		out[i] = filter.ParsedSort{Field: c}
	}
	return out
}

func (m *meta) keyWheres(ids []string) []*filter.Predicate {
	fields := m.e.GetFields()
	if m.ch != nil {
		fields = entity.ValueFields(m.ch.Registry, m.e)
	}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := entity.KeyValues(id, m.keys, fields); err == nil {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	if len(m.keys) == 1 {
		return []*filter.Predicate{{Field: m.keys[0], Op: filter.OpIn, Values: valid}}
	}
	per := 63 / (1 + len(m.keys))
	var out []*filter.Predicate
	for _, id := range valid {
		parts, _ := entity.SplitKey(id, len(m.keys))
		if len(out) == 0 || len(out[len(out)-1].Children) == per {
			out = append(out, &filter.Predicate{Or: true})
		}
		rec := filter.Predicate{Children: make([]filter.Predicate, len(parts))}
		for i, c := range m.keys {
			rec.Children[i] = filter.Predicate{Field: c, Op: filter.OpEq, Value: parts[i]}
		}
		or := out[len(out)-1]
		or.Children = append(or.Children, rec)
	}
	return out
}

func (m *meta) getRow(ctx context.Context, id string) (map[string]any, error) {
	return m.ch.GetOne(crud.WithReadHooks(ctx), id, nil)
}

func (m *meta) getRowUnhooked(ctx context.Context, id string) (map[string]any, error) {
	return m.ch.GetOne(ctx, id, nil)
}

func (m *meta) listRows(ctx context.Context, opts crud.ListOptions) ([]map[string]any, error) {
	return m.ch.ListAll(crud.WithReadHooks(ctx), opts)
}

func (m *meta) listRowsUnhooked(ctx context.Context, opts crud.ListOptions) ([]map[string]any, error) {
	return m.ch.ListAll(ctx, opts)
}

func (m *meta) countRows(ctx context.Context, opts crud.ListOptions) (int, error) {
	return m.ch.CountAll(crud.WithReadHooks(ctx), opts)
}

func (m *meta) sumRows(ctx context.Context, field string, opts crud.ListOptions) (string, error) {
	return m.ch.SumAll(crud.WithReadHooks(ctx), field, opts)
}

func (m *meta) groupRows(ctx context.Context, field string, opts crud.ListOptions, limit int) ([]crud.GroupCount, error) {
	return m.ch.GroupCountAll(crud.WithReadHooks(ctx), field, opts, limit)
}

func (m *meta) listByIDs(ctx context.Context, ids []string, opts crud.ListOptions) ([]map[string]any, error) {
	return m.listByIDsWith(ctx, ids, opts, m.listRows)
}

func (m *meta) listByIDsUnhooked(ctx context.Context, ids []string, opts crud.ListOptions) ([]map[string]any, error) {
	return m.listByIDsWith(ctx, ids, opts, m.listRowsUnhooked)
}

func (m *meta) listByIDsWith(ctx context.Context, ids []string, opts crud.ListOptions, read func(context.Context, crud.ListOptions) ([]map[string]any, error)) ([]map[string]any, error) {
	asked := make(map[string]bool, len(ids))
	for _, id := range ids {
		asked[id] = true
	}
	var out []map[string]any
	for _, w := range m.keyWheres(ids) {
		o := opts
		o.Where, o.Limit = w, len(ids)
		rows, err := read(ctx, o)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if id := m.rowID(row); asked[id] {
				delete(asked, id)
				out = append(out, row)
			}
		}
	}
	return out, nil
}

func (m *meta) field(name string) (schema.Field, bool) {
	f, ok := m.byName[name]
	return f, ok
}

func (m *meta) hint(field string) entity.FieldDisplay { return m.d.Fields[field] }

func (m *meta) inputName(column string) string {
	return m.ch.WireKey(column)
}

func (m *meta) singular(ctx context.Context) string {
	return i18nui.EntitySingular(ctx, m.tr, m.name, m.d.Singular)
}

func (m *meta) plural(ctx context.Context) string {
	return i18nui.EntityPlural(ctx, m.tr, m.name, m.d.Plural)
}

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

func (m *meta) system(f schema.Field) bool {
	if f.AutoGenerate != schema.AutoNone || f.ReadOnly {
		return true
	}
	return f.Name == m.pk && !f.Required
}

func (m *meta) guarded(field string) bool {
	if m.states == nil {
		return false
	}
	return slices.Contains(m.states.Guarded(), field)
}

func (m *meta) locked(f schema.Field) bool {
	return m.createLocked(f) || m.isKey(f.Name)
}

func (m *meta) createLocked(f schema.Field) bool {
	return m.hint(f.Name).Locked || m.guarded(f.Name) || m.system(f) || m.serverOwned(f.Name)
}

func (m *meta) serverOwned(field string) bool {
	sc := m.e.Config.Scope
	if sc == nil {
		return false
	}
	if sc.OwnerField != "" && field == sc.OwnerField {
		return true
	}
	return sc.MultiTenant && field == m.e.Config.TenantColumn()
}

func (m *meta) omitted(field string) bool { return m.hint(field).Omit }

func (m *meta) titleFields() []string {
	if len(m.d.TitleFields) > 0 {
		return m.d.TitleFields
	}
	for _, n := range []string{"name", "title"} {
		if _, ok := m.byName[n]; ok {
			return []string{n}
		}
	}
	for _, f := range m.fields {
		if f.Type == schema.String && !f.NoQuery && !m.system(f) && !m.omitted(f.Name) {
			return []string{f.Name}
		}
	}
	return nil
}

func (m *meta) titleField() string {
	if tf := m.titleFields(); len(tf) > 0 {
		return tf[0]
	}
	return ""
}

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

func (m *meta) rowValue(row map[string]any, key string) any {
	if w, ok := m.wire[key]; ok {
		if v, ok := row[w]; ok {
			return v
		}
	}
	if v, ok := row[key]; ok {
		return v
	}
	return row[casing.ToCamel(key)]
}
