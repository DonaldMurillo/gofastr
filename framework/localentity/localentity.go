// Package localentity declares records that live in the visitor's
// browser and renders the forms and lists that edit them, with no
// server table and no JavaScript in the app.
//
//	var Team = localdb.New("team")
//	var Members = localentity.Define(Team, "members", []schema.Field{
//	    {Name: "name", Type: schema.String, Required: true, Max: ptr(20)},
//	    {Name: "level", Type: schema.Int, Min: ptr(1), Max: ptr(100)},
//	}, localentity.Indexed("level"), localentity.MaxRecords(6))
//
// A declared entity is a core-ui/localdb store with three built-in
// fields: id (a UUIDv7 string minted in the browser), created_at and
// updated_at (RFC 3339 strings). Its forms and lists render through
// the design system on the server, like every other page; the
// localentity-form behaviour then saves form submissions into
// IndexedDB, and the localentity behaviour fills each list by cloning
// its server-rendered row template, setting text only. Every tab of the site re-renders when any tab writes.
//
// Nothing here talks to the server after the page loads, and the
// server never sees the records. A later sync layer can carry them to
// a real database; the declarations use core/schema field types so
// the same fields can describe a server entity.
package localentity

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"

	_ "embed"
)

// The built-in fields every local entity carries.
const (
	FieldID        = "id"
	FieldCreatedAt = "created_at"
	FieldUpdatedAt = "updated_at"
)

//go:embed localentity.js
var listJS string

//go:embed localform.js
var formJS string

// Two behaviours, so a page that only shows records never downloads
// the form code: the list module loads on a local list or count after
// the localdb adapter, the form module on a form carrier after localdb
// and the formerrors module that places refusals beside fields. A
// list's Edit control hands the record to the form module, which the
// form carrier it names has loaded.
var (
	_ = registry.RegisterBehavior("localentity", listJS,
		registry.Markers("[data-fui-local-list]", "[data-fui-local-count]"),
		registry.Requires("localdb"))
	_ = registry.RegisterBehavior("localentity-form", formJS,
		registry.Markers("[data-fui-local-form]"),
		registry.Requires("localdb", "formerrors"))
)

// The list and form carriers are behaviour hooks, not boxes: they lay
// out as if absent, so a list's rows are direct layout children of
// the ui.Grid or ui.Stack around it, and a form wrapper adds nothing
// around the ui.Form inside.
var carrierStyle = registry.RegisterStyle("local-entity", func(style.Theme) string {
	return `[data-fui-local-list],[data-fui-local-form]{display:contents}`
})

// Entity is one declared local entity.
type Entity struct {
	target string // "<db>/<store>", the attribute value the behaviour reads
	store  *localdb.Store
	fields []schema.Field
	index  map[string]bool // fields with an index, built-ins included
	spec   string          // JSON the form carrier hands the behaviour
	failed string          // Messages.Failed, for a list's delete
}

// Option configures Define.
type Option func(*options)

type options struct {
	indexed  []string
	max      int
	messages Messages
}

// Indexed adds an IndexedDB index over each named field, so a list can
// order by it (ListConfig.OrderBy). created_at and updated_at are
// always indexed. A Bool field cannot be indexed: IndexedDB keys are
// never booleans.
func Indexed(fields ...string) Option {
	return func(o *options) { o.indexed = append(o.indexed, fields...) }
}

// MaxRecords caps how many records the entity holds. A create past
// the cap is refused with Messages.Full and nothing is written; edits
// still work. 0 means no cap.
func MaxRecords(n int) Option {
	return func(o *options) { o.max = n }
}

// WithMessages replaces the validation messages the behaviour shows.
// Empty fields keep their defaults. {n} is replaced by the bound.
func WithMessages(m Messages) Option {
	return func(o *options) { o.messages = m }
}

// Messages are the words the behaviour places beside a field (or
// toasts, for Full and Failed) when a submission is refused. The
// browser's own validation runs first on any control the form renders
// with required/min/max/maxlength/pattern; these cover the rest and a
// form rendered without them.
type Messages struct {
	Required string `json:"required"`
	Number   string `json:"number"`
	Integer  string `json:"integer"`
	Min      string `json:"min"`
	Max      string `json:"max"`
	MinLen   string `json:"minlen"`
	MaxLen   string `json:"maxlen"`
	Pattern  string `json:"pattern"`
	Choice   string `json:"choice"`
	Date     string `json:"date"`
	Full     string `json:"full"`
	Failed   string `json:"failed"`
}

// DefaultMessages are the English defaults.
var DefaultMessages = Messages{
	Required: "This field is required.",
	Number:   "Enter a number.",
	Integer:  "Enter a whole number.",
	Min:      "Must be at least {n}.",
	Max:      "Must be at most {n}.",
	MinLen:   "Must be at least {n} characters.",
	MaxLen:   "Must be at most {n} characters.",
	Pattern:  "Use the requested format.",
	Choice:   "Choose one of the listed options.",
	Date:     "Enter a valid date.",
	Full:     "This list is full ({n} at most).",
	Failed:   "Could not save on this device.",
}

// fieldSpec is the per-field wire shape the behaviour validates
// against; the JSON tags are what localentity.js reads.
type fieldSpec struct {
	Name     string   `json:"name"`
	Type     string   `json:"t"`
	Required bool     `json:"req,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Values   []string `json:"values,omitempty"`
	Default  any      `json:"def,omitempty"`
}

type entitySpec struct {
	Fields   []fieldSpec `json:"fields"`
	Max      int         `json:"max,omitempty"`
	Messages Messages    `json:"msgs"`
}

// typeNames maps the core/schema types a local entity accepts to the
// behaviour's type tags. Relation, JSON, Decimal, UUID, Image and File
// need machinery a browser-only record does not have (a target table,
// exact arithmetic, an upload) and are refused at Define.
var typeNames = map[schema.FieldType]string{
	schema.String:    "string",
	schema.Text:      "text",
	schema.Int:       "int",
	schema.Float:     "float",
	schema.Bool:      "bool",
	schema.Enum:      "enum",
	schema.Date:      "date",
	schema.Timestamp: "timestamp",
}

// Define declares a local entity named name in db, with the given
// fields, and returns it. It declares the backing store itself (keyed
// by id, created_at and updated_at indexed, plus Indexed fields), so
// name must not already be a store in db. Field names are lowercase
// snake_case ([a-z][a-z0-9_]*, at most 61 bytes, so "by_<name>" is a
// valid index name) and must not reuse a built-in. Every refusal is a
// panic at startup.
func Define(db *localdb.DB, name string, fields []schema.Field, opts ...Option) *Entity {
	if db == nil {
		panic("localentity: Define needs a localdb.DB")
	}
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	if o.max < 0 {
		panic(fmt.Sprintf("localentity: %s: MaxRecords must not be negative", name))
	}
	if len(fields) == 0 {
		panic(fmt.Sprintf("localentity: %s: declare at least one field", name))
	}

	e := &Entity{index: map[string]bool{FieldCreatedAt: true, FieldUpdatedAt: true}}
	spec := entitySpec{Max: o.max, Messages: mergeMessages(o.messages)}
	seen := map[string]bool{}
	for _, f := range fields {
		if !validField(f.Name) {
			panic(fmt.Sprintf("localentity: %s: field name %q must be lowercase snake_case, 1-61 bytes", name, f.Name))
		}
		if f.Name == FieldID || f.Name == FieldCreatedAt || f.Name == FieldUpdatedAt {
			panic(fmt.Sprintf("localentity: %s: field %q is built in", name, f.Name))
		}
		if seen[f.Name] {
			panic(fmt.Sprintf("localentity: %s: field %q declared twice", name, f.Name))
		}
		seen[f.Name] = true
		t, ok := typeNames[f.Type]
		if !ok {
			panic(fmt.Sprintf("localentity: %s: field %q has a type a local entity cannot store (use String, Text, Int, Float, Bool, Enum, Date or Timestamp)", name, f.Name))
		}
		if f.Type == schema.Enum && len(f.Values) == 0 {
			panic(fmt.Sprintf("localentity: %s: enum field %q lists no Values", name, f.Name))
		}
		spec.Fields = append(spec.Fields, fieldSpec{
			Name: f.Name, Type: t, Required: f.Required, Min: f.Min, Max: f.Max,
			Pattern: f.Pattern, Values: f.Values, Default: f.Default,
		})
	}

	storeOpts := []localdb.StoreOption{
		localdb.AutoKey(),
		localdb.Index(indexName(FieldCreatedAt), FieldCreatedAt),
		localdb.Index(indexName(FieldUpdatedAt), FieldUpdatedAt),
	}
	for _, f := range o.indexed {
		def, ok := fieldByName(fields, f)
		if !ok {
			panic(fmt.Sprintf("localentity: %s: Indexed names unknown field %q", name, f))
		}
		if def.Type == schema.Bool {
			panic(fmt.Sprintf("localentity: %s: Bool field %q cannot be indexed", name, f))
		}
		if e.index[f] {
			continue
		}
		e.index[f] = true
		storeOpts = append(storeOpts, localdb.Index(indexName(f), f))
	}
	e.store = db.Store(name, storeOpts...)
	e.target = db.Name() + "/" + name
	e.fields = slices.Clone(fields)

	buf, err := json.Marshal(spec)
	if err != nil {
		panic(fmt.Sprintf("localentity: %s: field defaults must be JSON values: %v", name, err))
	}
	e.spec = string(buf)
	e.failed = spec.Messages.Failed
	return e
}

// Name returns the entity's store name.
func (e *Entity) Name() string { return e.store.Name() }

// Store returns the backing localdb store, for page scripts that read
// the records through __gofastr.localdb directly.
func (e *Entity) Store() *localdb.Store { return e.store }

// Fields returns a copy of the declared fields (built-ins excluded).
func (e *Entity) Fields() []schema.Field { return slices.Clone(e.fields) }

func (e *Entity) known(field string) bool {
	if field == FieldID || field == FieldCreatedAt || field == FieldUpdatedAt {
		return true
	}
	_, ok := fieldByName(e.fields, field)
	return ok
}

// ListConfig configures a List.
type ListConfig struct {
	// OrderBy is the field rows are ordered by: created_at (the
	// default), updated_at, or a field declared Indexed. IndexedDB
	// does the ordering through the index; page script never sorts.
	OrderBy string
	// Desc reverses the order (newest first, highest first).
	Desc bool
	// Limit caps how many rows render. 0 renders every record.
	Limit int
}

// List is a configured list of an entity's records. Compose it inside
// a layout component: the rows become that component's children.
//
//	l := Members.List(localentity.ListConfig{OrderBy: "level", Desc: true})
//	ui.Grid(ui.GridConfig{Min: "14rem"}, l.Render(
//	    l.Row(func(r localentity.Row) render.HTML { return ui.Card(...) }),
//	    l.Empty(ui.EmptyState(...)),
//	))
type List struct {
	e   *Entity
	cfg ListConfig
}

// List returns a list over the entity's records.
func (e *Entity) List(cfg ListConfig) List {
	if cfg.OrderBy == "" {
		cfg.OrderBy = FieldCreatedAt
	}
	if !e.index[cfg.OrderBy] {
		panic(fmt.Sprintf("localentity: %s: OrderBy %q needs an index (declare it with Indexed)", e.Name(), cfg.OrderBy))
	}
	if cfg.Limit < 0 {
		panic(fmt.Sprintf("localentity: %s: Limit must not be negative", e.Name()))
	}
	return List{e: e, cfg: cfg}
}

// Render returns the list carrier holding its row and empty
// templates. Until the behaviour fills it, it shows nothing: the
// server cannot see the visitor's records.
func (l List) Render(row, empty render.HTML) render.HTML {
	attrs := html.Attrs{
		"data-fui-local-list":  l.e.target,
		"data-fui-local-order": indexName(l.cfg.OrderBy),
		"data-fui-local-fail":  l.e.failed,
	}
	if l.cfg.Desc {
		attrs["data-fui-local-dir"] = "prev"
	}
	if l.cfg.Limit > 0 {
		attrs["data-fui-local-limit"] = strconv.Itoa(l.cfg.Limit)
	}
	return carrierStyle.WrapHTML(html.Div(html.DivConfig{ExtraAttrs: attrs}, row, empty))
}

// Row renders the row template. fn builds one row from the design
// system; Row's helpers mark where each record's values go. The
// template must have exactly one root element, and should carry no
// id attributes: every record clones it.
func (l List) Row(fn func(Row) render.HTML) render.HTML {
	return html.Template(html.TemplateConfig{ExtraAttrs: html.Attrs{"data-fui-local-row": ""}}, fn(Row{e: l.e}))
}

// Empty renders what the list shows when it has no records. One root
// element, like Row.
func (l List) Empty(content render.HTML) render.HTML {
	return html.Template(html.TemplateConfig{ExtraAttrs: html.Attrs{"data-fui-local-empty": ""}}, content)
}

// Row marks where a record's values go inside a row template.
type Row struct{ e *Entity }

// Text renders a span the behaviour fills with the record's value for
// field, as text (never markup). Built-ins are allowed: id,
// created_at, updated_at. An unknown field panics.
func (r Row) Text(field string) render.HTML {
	if !r.e.known(field) {
		panic(fmt.Sprintf("localentity: %s: Row.Text names unknown field %q", r.e.Name(), field))
	}
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-text": field}})
}

// Delete wraps a control (a ui.Button with Type "button") so a click
// deletes this row's record.
func (r Row) Delete(control render.HTML) render.HTML {
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-delete": ""}}, control)
}

// Edit wraps a control so a click loads this row's record into the
// entity form whose carrier id is formID (Entity.Form); the next save
// of that form updates the record instead of creating one, and the
// form's reset button ends the edit.
func (r Row) Edit(formID string, control render.HTML) render.HTML {
	if formID == "" {
		panic(fmt.Sprintf("localentity: %s: Row.Edit needs the form carrier's id", r.e.Name()))
	}
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-edit": formID}}, control)
}

// Form wraps a design-system form (ui.Form) so its submissions are
// saved into this entity instead of posted. Each control's name is a
// field name; controls with other names (the CSRF input, a button)
// are ignored. id names the carrier, for Row.Edit. The browser's own
// constraint validation runs first; the behaviour then coerces and
// checks each value against the field declarations and places any
// refusal beside its field.
//
// ui.Form still needs an Action: without JavaScript the form posts
// there natively, so point it at a route that explains the page needs
// script, or at the page itself.
func (e *Entity) Form(id string, form render.HTML) render.HTML {
	return carrierStyle.WrapHTML(html.Div(html.DivConfig{ID: id, ExtraAttrs: html.Attrs{
		"data-fui-local-form":   e.target,
		"data-fui-local-schema": e.spec,
	}}, form))
}

// Count renders a span the behaviour fills with how many records the
// entity holds, kept current across tabs.
func (e *Entity) Count() render.HTML {
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-count": e.target}})
}

func indexName(field string) string { return "by_" + field }

func fieldByName(fields []schema.Field, name string) (schema.Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return schema.Field{}, false
}

// validField is lowercase snake_case, 1-61 bytes: "by_" + name stays a
// valid localdb index name, and no name can spell a prototype key.
func validField(s string) bool {
	if s == "" || len(s) > 61 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func mergeMessages(m Messages) Messages {
	d := DefaultMessages
	pick := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	pick(&d.Required, m.Required)
	pick(&d.Number, m.Number)
	pick(&d.Integer, m.Integer)
	pick(&d.Min, m.Min)
	pick(&d.Max, m.Max)
	pick(&d.MinLen, m.MinLen)
	pick(&d.MaxLen, m.MaxLen)
	pick(&d.Pattern, m.Pattern)
	pick(&d.Choice, m.Choice)
	pick(&d.Date, m.Date)
	pick(&d.Full, m.Full)
	pick(&d.Failed, m.Failed)
	return d
}
