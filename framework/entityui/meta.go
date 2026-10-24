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
	keys   []string          // the primary-key columns: a record's id is rowID
	pk     string            // the one key column a relation joins on; "" for a composite key
	wire   map[string]string // column -> WireName, for the fields that set one
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
	// Screens name a record by its whole key; only a relation, refused
	// onto or off a composite key at boot, reads the one column.
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

// isKey reports whether field is a primary-key column.
func (m *meta) isKey(field string) bool { return slices.Contains(m.keys, field) }

// rowID is a row's record id, entity.JoinKey over its key columns, or ""
// when a key value is missing.
func (m *meta) rowID(row map[string]any) string {
	parts := make([]string, len(m.keys))
	for i, c := range m.keys {
		if parts[i] = cell(m.rowValue(row, c)); parts[i] == "" {
			return ""
		}
	}
	return entity.JoinKey(parts)
}

// idPath is a record id as the path segments that name it: the escaped
// id for a one-column key, each value escaped for a composite key.
func (m *meta) idPath(id string) string {
	if len(m.keys) > 1 {
		if parts, err := entity.SplitKey(id, len(m.keys)); err == nil {
			return entity.JoinKey(parts)
		}
	}
	return url.PathEscape(id)
}

// keySorts orders rows by the primary key.
func (m *meta) keySorts() []filter.ParsedSort {
	out := make([]filter.ParsedSort, len(m.keys))
	for i, c := range m.keys {
		out[i] = filter.ParsedSort{Field: c}
	}
	return out
}

// keyWheres returns the predicates matching the records ids name: one IN
// for a one-column key; for a composite key, ORs of per-record ANDs, as
// many records to each as keep its tree under the filter's 64-node cap.
// An id entity.KeyValues refuses names no record: one that does not
// split into the key, or an integer value not in canonical decimal,
// which the database would coerce onto another record's key.
func (m *meta) keyWheres(ids []string) []*filter.Predicate {
	fields := m.e.GetFields()
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

// listByIDs reads the records ids name, with opts' fields and sorts, one
// keyWheres predicate at a time: rows come in key order within each.
// opts' fields must carry the key columns: a row whose own id is not
// one of ids drops out, since a column type the database chose (a
// Postgres UUID or CITEXT under a view or an Unmanaged table) matches a
// second spelling of its key. Each record comes back once: an alias in
// one predicate reaches the row its own id reaches in another.
func (m *meta) listByIDs(ctx context.Context, ids []string, opts crud.ListOptions) ([]map[string]any, error) {
	asked := make(map[string]bool, len(ids))
	for _, id := range ids {
		asked[id] = true
	}
	var out []map[string]any
	for _, w := range m.keyWheres(ids) {
		o := opts
		o.Where, o.Limit = w, len(ids)
		rows, err := m.ch.ListAll(ctx, o)
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

// system reports the fields the server owns outright: a one-column
// primary key, auto-generated fields and ReadOnly fields. The record
// header shows them; forms never carry them. A composite key's columns
// name the record the caller creates (the pair a join row links), so a
// create sets them and every later write leaves them (locked).
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
// Locked, guarded, system, or a key column (a record keeps its key).
func (m *meta) locked(f schema.Field) bool {
	return m.createLocked(f) || m.isKey(f.Name)
}

// createLocked is locked for a create form, which sets a composite key.
func (m *meta) createLocked(f schema.Field) bool {
	return m.hint(f.Name).Locked || m.guarded(f.Name) || m.system(f)
}

// omitted reports a field left out of forms and columns on purpose.
func (m *meta) omitted(field string) bool { return m.hint(field).Omit }

// titleFields are the fields naming a record: Display.TitleFields,
// else a visible "name" or "title" field, else the first String column
// that is not system, omitted or NoQuery (an invoice's number), else
// none (the singular names it). rowTitles joins their values.
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

// titleField is the first title field, "" when there is none: the
// column that carries a list's record link, stays pinned among the
// columns and titles a card.
func (m *meta) titleField() string {
	if tf := m.titleFields(); len(tf) > 0 {
		return tf[0]
	}
	return ""
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

// rowValue reads a row value by declaration name: under the field's
// WireName when it sets one (the key the CRUD handler returns it under),
// then the name itself, then the camelCase spelling the JSON API
// serializes under camel casing.
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
