// Package localentity declares records kept in the visitor's browser (Define)
// and renders the forms and lists that edit them (Entity.Form, Entity.List).
package localentity

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"

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

var (
	_ = registry.RegisterBehavior("localentity", listJS,
		registry.Markers("[data-fui-local-list]", "[data-fui-local-count]"),
		registry.Requires("localdb"))
	_ = registry.RegisterBehavior("localentity-form", formJS,
		registry.Markers("[data-fui-local-form]"),
		registry.Requires("localdb", "formerrors"),
		registry.Interactions(registry.Interaction{Event: "click", Selector: `[data-fui-local-form] [type="submit"]`}))
)

var carrierStyle = registry.RegisterStyle("local-entity", func(style.Theme) string {
	return `[data-fui-local-list],[data-fui-local-form]{display:contents}`
})

type Entity struct {
	target string // "<db>/<store>", the attribute value the behaviour reads
	store  *localdb.Store
	fields []schema.Field
	index  map[string]bool // fields with an index, built-ins included
	spec   string          // JSON the form carrier hands the behaviour
	failed string          // Messages.Failed, for a list's delete; "" says msgFailed
}

// Option configures Define.
type Option func(*options)

type options struct {
	indexed  []string
	max      int
	messages Messages
}

// Indexed indexes each named Required, non-Bool field so a list can order by it.
func Indexed(fields ...string) Option {
	return func(o *options) { o.indexed = append(o.indexed, fields...) }
}

// MaxRecords caps how many records the entity holds; 0 means no cap.
func MaxRecords(n int) Option {
	return func(o *options) { o.max = n }
}

// WithMessages replaces default validation messages; {{n}} is the bound.
func WithMessages(m Messages) Option {
	return func(o *options) { o.messages = m }
}

// Messages are the words shown when a submission is refused.
type Messages struct {
	Required string `json:"required,omitempty"`
	Number   string `json:"number,omitempty"`
	Integer  string `json:"integer,omitempty"`
	Min      string `json:"min,omitempty"`
	Max      string `json:"max,omitempty"`
	MinLen   string `json:"minlen,omitempty"`
	MaxLen   string `json:"maxlen,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
	Choice   string `json:"choice,omitempty"`
	Date     string `json:"date,omitempty"`
	Full     string `json:"full,omitempty"`
	Failed   string `json:"failed,omitempty"`
}

var (
	msgRequired = i18n.Define("localentity.required", "This field is required.")
	msgNumber   = i18n.Define("localentity.number", "Enter a number.")
	msgInteger  = i18n.Define("localentity.integer", "Enter a whole number.")
	msgMin      = i18n.Define("localentity.min", "Must be at least {{n}}.")
	msgMax      = i18n.Define("localentity.max", "Must be at most {{n}}.")
	msgMinLen   = i18n.Define("localentity.minlen", "Must be at least {{n}} characters.")
	msgMaxLen   = i18n.Define("localentity.maxlen", "Must be at most {{n}} characters.")
	msgPattern  = i18n.Define("localentity.pattern", "Use the requested format.")
	msgChoice   = i18n.Define("localentity.choice", "Choose one of the listed options.")
	msgDate     = i18n.Define("localentity.date", "Enter a valid date.")
	msgFull     = i18n.Define("localentity.full", "This list is full ({{n}} at most).")
	msgFailed   = i18n.Define("localentity.failed", "Could not save on this device.")
)

func init() {
	runtime.RegisterStrings(msgRequired, msgNumber, msgInteger, msgMin, msgMax, msgMinLen,
		msgMaxLen, msgPattern, msgChoice, msgDate, msgFull, msgFailed)
}

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
	Messages Messages    `json:"msgs"` // the app's overrides; the rest are phrases
}

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

// maxSafeInteger is the bound of JavaScript's Number.isSafeInteger,
// the whole-number rule the browser's save path applies to an Int.
const maxSafeInteger = float64(1<<53 - 1)

// Define declares a local entity and its store in db; refusals panic.
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
	spec := entitySpec{Max: o.max, Messages: o.messages}
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
		if f.Pattern != "" {
			if err := checkPattern(f.Pattern); err != nil {
				panic(fmt.Sprintf("localentity: %s: field %q: %v", name, f.Name, err))
			}
		}
		if f.Type == schema.Enum && len(f.Values) == 0 {
			panic(fmt.Sprintf("localentity: %s: enum field %q lists no Values", name, f.Name))
		}
		def, err := validDefault(f)
		if err != nil {
			panic(fmt.Sprintf("localentity: %s: field %q: %v", name, f.Name, err))
		}
		spec.Fields = append(spec.Fields, fieldSpec{
			Name: f.Name, Type: t, Required: f.Required, Min: f.Min, Max: f.Max,
			Pattern: f.Pattern, Values: f.Values, Default: def,
		})
	}

	storeOpts := []localdb.StoreOption{
		localdb.AutoKey(),
		localdb.Index(indexName(FieldCreatedAt), FieldCreatedAt),
		localdb.Index(indexName(FieldUpdatedAt), FieldUpdatedAt),
	}
	for _, f := range o.indexed {
		if f == FieldCreatedAt || f == FieldUpdatedAt {
			continue // always indexed
		}
		def, ok := fieldByName(fields, f)
		if !ok {
			panic(fmt.Sprintf("localentity: %s: Indexed names unknown field %q", name, f))
		}
		if def.Type == schema.Bool {
			panic(fmt.Sprintf("localentity: %s: Bool field %q cannot be indexed", name, f))
		}
		if !def.Required {
			panic(fmt.Sprintf("localentity: %s: Indexed field %q must be Required: a record without it would vanish from lists ordered by it", name, f))
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

// Store returns the backing localdb store.
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
	// OrderBy is created_at (the default), updated_at or an Indexed field.
	OrderBy string
	// Desc reverses the order (newest first, highest first).
	Desc bool
	// Limit caps how many rows render; 0 renders every record.
	Limit int
}

// List is a configured list of an entity's records.
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

// Render returns the list carrier holding its row and empty templates.
func (l List) Render(row, empty render.HTML) render.HTML {
	attrs := html.Attrs{
		"data-fui-local-list":  l.e.target,
		"data-fui-local-order": indexName(l.cfg.OrderBy),
	}
	if l.e.failed != "" {
		attrs["data-fui-local-fail"] = l.e.failed
	}
	if l.cfg.Desc {
		attrs["data-fui-local-dir"] = "prev"
	}
	if l.cfg.Limit > 0 {
		attrs["data-fui-local-limit"] = strconv.Itoa(l.cfg.Limit)
	}
	return carrierStyle.WrapHTML(html.Div(html.DivConfig{ExtraAttrs: attrs}, row, empty))
}

// Row renders the row template: one root element, no id attributes.
func (l List) Row(fn func(Row) render.HTML) render.HTML {
	return html.Template(html.TemplateConfig{ExtraAttrs: html.Attrs{"data-fui-local-row": ""}}, fn(Row{e: l.e}))
}

// Empty renders what the list shows with no records; one root element.
func (l List) Empty(content render.HTML) render.HTML {
	return html.Template(html.TemplateConfig{ExtraAttrs: html.Attrs{"data-fui-local-empty": ""}}, content)
}

// Row marks where a record's values go inside a row template.
type Row struct{ e *Entity }

// Text renders a span filled with the record's value for field, as text.
func (r Row) Text(field string) render.HTML {
	if !r.e.known(field) {
		panic(fmt.Sprintf("localentity: %s: Row.Text names unknown field %q", r.e.Name(), field))
	}
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-text": field}})
}

// Delete wraps a control so a click deletes this row's record.
func (r Row) Delete(control render.HTML) render.HTML {
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-delete": ""}}, control)
}

// Edit wraps a control that loads this row's record into form formID.
func (r Row) Edit(formID string, control render.HTML) render.HTML {
	if formID == "" {
		panic(fmt.Sprintf("localentity: %s: Row.Edit needs the form carrier's id", r.e.Name()))
	}
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-edit": formID}}, control)
}

// Form wraps a ui.Form so its submissions save into this entity.
func (e *Entity) Form(id string, form render.HTML) render.HTML {
	return carrierStyle.WrapHTML(html.Div(html.DivConfig{ID: id, ExtraAttrs: html.Attrs{
		"data-fui-local-form":   e.target,
		"data-fui-local-schema": e.spec,
	}}, form))
}

// Count renders a span filled with how many records the entity holds.
func (e *Entity) Count() render.HTML {
	return html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-fui-local-count": e.target}})
}

func indexName(field string) string { return "by_" + field }

func checkPattern(p string) error {
	if _, err := regexp.Compile(p); err != nil {
		return fmt.Errorf("Pattern does not compile: %v", err)
	}
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\\':
			if i+1 >= len(p) {
				return fmt.Errorf("Pattern ends in a lone backslash")
			}
			n := p[i+1]
			switch {
			case n == 's' || n == 'S':
				return fmt.Errorf("Pattern uses \\%c: JavaScript's \\s is Unicode whitespace, Go's is ASCII [ \\t\\n\\f\\r]; name the characters in a class instead (a space, \\t, \\r, \\n, \\f, \\v …)", n)
			case strings.IndexByte(`dDwWnrtfv\/.^$*+?()[]{}|`, n) >= 0:
			case (n == 'b' || n == 'B') && !inClass:
			case n == '-' && inClass:
			case n == 'p' || n == 'P':
				end := strings.IndexByte(p[i:], '}')
				if i+2 >= len(p) || p[i+2] != '{' || end < 0 || !generalCategory[p[i+3:i+end]] {
					return fmt.Errorf("Pattern uses \\%c without a general category in braces (\\%c{L}, \\%c{Nd}, …)", n, n, n)
				}
				i += end - 1
			default:
				return fmt.Errorf("Pattern uses \\%c, which JavaScript and Go do not read the same way", n)
			}
			i++
		case inClass:
			if c == '[' && i+1 < len(p) && p[i+1] == ':' {
				return fmt.Errorf("Pattern uses a POSIX class; JavaScript does not have them")
			}
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
			if strings.HasPrefix(p[i+1:], "^") {
				i++
			}
			if i+1 < len(p) && p[i+1] == ']' {
				return fmt.Errorf("Pattern opens a class with ]; escape it as \\]")
			}
		case c == '.' && !inClass:
			return fmt.Errorf("Pattern uses . outside a class: JavaScript's . skips every line terminator (\\n \\r \\u2028 \\u2029), Go's only \\n; name the characters you accept in a class instead")
		case c == '(' && strings.HasPrefix(p[i:], "(?") && !strings.HasPrefix(p[i:], "(?:"):
			return fmt.Errorf("Pattern uses %q; JavaScript has no inline flags or (?P<name>) groups", p[i:min(i+4, len(p))])
		case c == '{':
			q := quantifier.FindString(p[i:])
			if q == "" {
				return fmt.Errorf("Pattern has a { that is not a {n,m} quantifier; escape it as \\{")
			}
			i += len(q) - 1
		case c == '}' || c == ']':
			return fmt.Errorf("Pattern has a lone %c; escape it", c)
		}
	}
	return nil
}

var quantifier = regexp.MustCompile(`^\{\d+(,\d*)?\}`)

var timestampDefaultSyntax = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// validDefault checks a Default the way the form checks a submitted
// value, and returns it as the browser stores it.
func validDefault(f schema.Field) (any, error) {
	if f.Default == nil {
		return nil, nil
	}
	b, err := json.Marshal(f.Default)
	if err != nil {
		return nil, fmt.Errorf("field defaults must be JSON values: %w", err)
	}
	var v any
	if err := jsonexact.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("field defaults must be JSON values: %w", err)
	}
	if v == nil {
		return nil, nil
	}
	switch f.Type {
	case schema.Bool:
		if _, ok := v.(bool); !ok {
			return nil, badDefault(v, "a Bool")
		}
	case schema.Int, schema.Float:
		n, ok := v.(float64)
		if wide, isWide := v.(json.Number); isWide {
			if f.Type == schema.Int {
				return nil, badDefault(v, "a safe integer")
			}
			n, err = wide.Float64()
			ok = err == nil
		}
		if !ok {
			return nil, badDefault(v, "a number")
		}
		if f.Type == schema.Int && (n != math.Trunc(n) || math.Abs(n) > maxSafeInteger) {
			return nil, badDefault(v, "a whole number")
		}
		if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return nil, fmt.Errorf("default %v is outside Min and Max", v)
		}
		return n, nil
	case schema.Date:
		s, ok := v.(string)
		if !ok {
			return nil, badDefault(v, "a YYYY-MM-DD date")
		}
		if d, err := time.Parse("2006-01-02", s); err != nil || d.Format("2006-01-02") != s {
			return nil, fmt.Errorf("default %q is not a real calendar date", s)
		}
	case schema.Timestamp:
		s, ok := v.(string)
		if !ok {
			return nil, badDefault(v, "an RFC 3339 timestamp")
		}
		if !timestampDefaultSyntax.MatchString(s) {
			return nil, fmt.Errorf("default %q must use RFC 3339 syntax with a time zone", s)
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("default %q is not an RFC 3339 timestamp", s)
		}
		utc := t.UTC()
		if utc.Year() < 0 || utc.Year() > 9999 {
			return nil, fmt.Errorf("default %q must normalize to a four-digit UTC year", s)
		}
		return utc.Format("2006-01-02T15:04:05.000Z"), nil
	default:
		s, ok := v.(string)
		if !ok {
			return nil, badDefault(v, "a string")
		}
		if f.Required && s == "" {
			return nil, fmt.Errorf("required field cannot have an empty default")
		}
		if f.Type == schema.Enum && !slices.Contains(f.Values, s) {
			return nil, badDefault(v, "one of the field's Values")
		}
		n := utf8.RuneCountInString(s)
		if (f.Min != nil && float64(n) < *f.Min) || (f.Max != nil && float64(n) > *f.Max) {
			return nil, fmt.Errorf("default %q is outside Min and Max lengths", s)
		}
		if f.Pattern != "" {
			if ok, err := regexp.MatchString(f.Pattern, s); err != nil || !ok {
				return nil, fmt.Errorf("default %q does not match the field's Pattern", s)
			}
		}
	}
	return v, nil
}

func badDefault(v any, want string) error {
	return fmt.Errorf("default %v is not %s the form can save", v, want)
}

var generalCategory = map[string]bool{
	"L": true, "Lu": true, "Ll": true, "Lt": true, "Lm": true, "Lo": true,
	"M": true, "Mn": true, "Mc": true, "Me": true,
	"N": true, "Nd": true, "Nl": true, "No": true,
	"P": true, "Pc": true, "Pd": true, "Ps": true, "Pe": true, "Pi": true, "Pf": true, "Po": true,
	"S": true, "Sm": true, "Sc": true, "Sk": true, "So": true,
	"Z": true, "Zs": true, "Zl": true, "Zp": true,
	"C": true, "Cc": true, "Cf": true, "Co": true, "Cs": true,
}

func fieldByName(fields []schema.Field, name string) (schema.Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return schema.Field{}, false
}

func validField(s string) bool {
	if s == "" || len(s) > 61 || s[0] < 'a' || s[0] > 'z' || s == "constructor" || s == "prototype" {
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
