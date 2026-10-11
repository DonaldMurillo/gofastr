package entity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// StatesConfig names an entity's state field and the moves that change
// it. It is a rule the CRUD handler enforces, not a screen hint: unless
// Advisory is set, the state field and every stamp change only through
// CrudHandler.RunTransition, whatever the write comes from (REST, MCP,
// batch, cascade, upsert, a typed repo, a hook). A write that sends the
// stored value back is not a change and passes, so a PUT that round-trips
// the whole row still works. Trusted Go code that must write the field
// directly (seeds, imports, repair jobs) wraps its context in
// crud.WithStateOverride, which is audited and refused on an entity with
// no audit log.
//
// Like Scope and Display it is a pointer config: nil means the entity has
// no states, and Define copies it deeply.
type StatesConfig struct {
	// Field is the Enum field holding the state.
	Field string `json:"field"`
	// Initial lists the values a create may set. Empty means the field's
	// Default alone; a field with neither is refused at boot.
	Initial []string `json:"initial,omitempty"`
	// Transitions are the moves, in the order screens draw them.
	Transitions []Transition `json:"transitions,omitempty"`
	// Advisory releases the state field and the stamps to direct writes,
	// today's behaviour, and keeps the moves as calls and buttons. For an
	// entity whose status is not a business rule.
	Advisory bool `json:"advisory,omitempty"`
}

// Transition is one named move of the state field.
type Transition struct {
	// Key names the move in its route (POST <api>/<entity>/<id>/transitions/<key>)
	// and its MCP tool: lowercase segments joined by single underscores
	// (see transitionKeyGrammar), unique on the entity, and not one of the
	// names a generated surface already uses (reservedTransitionKeys).
	Key string `json:"key"`
	// Label is the button text. Empty draws the key.
	Label string `json:"label,omitempty"`
	// From lists the values the move starts from; never empty.
	From []string `json:"from"`
	// To is the value the move writes.
	To string `json:"to"`
	// Stamp names a Date or Timestamp field the move sets to the server's
	// current UTC date or time. The client never supplies it.
	Stamp string `json:"stamp,omitempty"`
	// Variant is the button variant screens draw the move with
	// (ui.ParseButtonVariant spellings). Empty is the screen's default.
	Variant string `json:"variant,omitempty"`
	// Permission, when set, is required on top of the entity's update
	// access. Empty means update access alone.
	Permission string `json:"permission,omitempty"`
	// System moves get no route, button or MCP tool; only Go code calls
	// RunTransition for them (a job marking invoices past due).
	System bool `json:"system,omitempty"`
}

// UnmarshalJSON decodes the states block strictly, moves included: an
// unknown key is an error, as it is in the display block.
func (s *StatesConfig) UnmarshalJSON(data []byte) error {
	type plain StatesConfig // sheds UnmarshalJSON so this does not recurse
	var p plain
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return err
	}
	*s = StatesConfig(p)
	return nil
}

// Transition returns the move named key, or false.
func (s *StatesConfig) Transition(key string) (Transition, bool) {
	if s == nil {
		return Transition{}, false
	}
	for _, t := range s.Transitions {
		if t.Key == key {
			return t, true
		}
	}
	return Transition{}, false
}

// Open returns the keys of the moves whose From holds value, in declared
// order, System moves included.
func (s *StatesConfig) Open(value string) []string {
	if s == nil {
		return nil
	}
	var keys []string
	for _, t := range s.Transitions {
		if slices.Contains(t.From, value) {
			keys = append(keys, t.Key)
		}
	}
	return keys
}

// Guarded returns the state field followed by every distinct stamp field,
// the columns a write may not change outside a move.
func (s *StatesConfig) Guarded() []string {
	if s == nil {
		return nil
	}
	cols := []string{s.Field}
	for _, t := range s.Transitions {
		if t.Stamp != "" && !slices.Contains(cols, t.Stamp) {
			cols = append(cols, t.Stamp)
		}
	}
	return cols
}

// InitialValues returns Initial, or the field's Default when Initial is
// empty.
func (s *StatesConfig) InitialValues(fields []schema.Field) []string {
	if s == nil {
		return nil
	}
	if len(s.Initial) > 0 {
		return s.Initial
	}
	for _, f := range fields {
		if f.Name == s.Field {
			if d, ok := f.Default.(string); ok && d != "" {
				return []string{d}
			}
		}
	}
	return nil
}

// transitionKeyGrammar is the shape every move key must follow: lowercase
// segments joined by single underscores, each segment led by a letter. It is
// strict enough that no two keys normalize onto one generated identifier:
// the emitters PascalCase a key by dropping the underscores and upper-casing
// the next segment's first letter, and that map is injective only when every
// segment starts with a letter and no underscore is doubled or trailing
// ("mark__paid" and "mark_paid" both become MarkPaid; "a_1" and "a1" both
// become A1). Display keys keep the looser displayKeyGrammar.
var transitionKeyGrammar = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z][a-z0-9]*)*$`)

// reservedTransitionKeys are the key spellings a generated surface already
// uses for every entity, so a move named one of them would shadow that
// surface's own name. The set is the union of the fixed names, each traced
// to the emitter that mints it (a move's own names are <entity>_<key> for
// the MCP tool, <key>_<Schema> for the OpenAPI operation id,
// toCamelCase(key)+<Entity> for the Go client method, <key> for the CLI
// subcommand and the JS SDK resource member):
//
//   - list, get, create, update, delete: the MCP tools <entity>_<verb>
//     (framework/crud/mcp.go), the OpenAPI ids <verb>_<Schema> and the Go
//     client methods <Verb><Entity> (framework/openapi/openapi.go,
//     cmd/gofastr/generate_client.go); also the CLI subcommands
//     (cmd/gofastr/generate_cli.go) and the JS resource members
//     (cmd/gofastr/generate_sdkjs.go).
//   - patch: the OpenAPI id patch_<Schema>, the Go client method
//     Patch<Entity>, the CLI subcommand and the JS member of the same
//     spelling (the MCP surface has no patch tool; the route is the
//     sparse-update twin of update).
//   - watch: the Go client method Watch<Entity>, the CLI subcommand and the
//     JS resource member for the SSE feed.
//   - batch_create, batch_update, batch_delete: the OpenAPI ids
//     batch_<verb>_<Schema>; toCamelCase maps them onto the Go client
//     methods BatchCreate/BatchUpdate/BatchDelete<Entity> (and the CLI
//     batch-verb wrappers of the same normalized name).
//   - events: the OpenAPI operation id events_<Schema> (the SSE
//     subscription stream).
//   - remove: the JS resource member — the SDK's spelling of delete.
//   - transition: the JS resource's generic move method,
//     client.<table>.transition(id, key), which every per-move member sits
//     beside; a move keyed transition would rebind it to a function that
//     calls itself.
//   - client, table, constructor: the JS resource's own instance fields
//     (the Client it calls through and its table path) and its
//     constructor. Each per-move member is an instance property, so a
//     move keyed client or table would overwrite the field every request
//     reads.
//
// The value names the collision in the boot error.
var reservedTransitionKeys = map[string]string{
	"list":         "the entity's own MCP tool <entity>_list, OpenAPI id list_<Schema> and client method List<Entity>",
	"get":          "the entity's own MCP tool <entity>_get, OpenAPI id get_<Schema> and client method Get<Entity>",
	"create":       "the entity's own MCP tool <entity>_create, OpenAPI id create_<Schema> and client method Create<Entity>",
	"update":       "the entity's own MCP tool <entity>_update, OpenAPI id update_<Schema> and client method Update<Entity>",
	"delete":       "the entity's own MCP tool <entity>_delete, OpenAPI id delete_<Schema> and client method Delete<Entity>",
	"patch":        "the entity's own OpenAPI id patch_<Schema> and client method Patch<Entity>",
	"watch":        "the entity's own client method Watch<Entity> and CLI subcommand watch",
	"batch_create": "the entity's own OpenAPI id batch_create_<Schema> and client method BatchCreate<Entity>",
	"batch_update": "the entity's own OpenAPI id batch_update_<Schema> and client method BatchUpdate<Entity>",
	"batch_delete": "the entity's own OpenAPI id batch_delete_<Schema> and client method BatchDelete<Entity>",
	"events":       "the entity's own OpenAPI id events_<Schema> (the SSE subscription)",
	"remove":       "the JS SDK resource's own remove member",
	"transition":   "the JS SDK resource's own transition method",
	"client":       "the JS SDK resource's own client field",
	"table":        "the JS SDK resource's own table field",
	"constructor":  "the JS SDK resource's constructor",
}

// validate checks every name the config holds against the entity's
// fields, so a typo fails the app at boot naming the offender.
func (s *StatesConfig) validate(c EntityConfig, pk string) error {
	name := c.Name
	byName := make(map[string]schema.Field, len(c.Fields))
	for _, f := range c.Fields {
		byName[f.Name] = f
	}
	if s.Field == "" {
		return fmt.Errorf("entity %q: states field is empty", name)
	}
	field, ok := byName[s.Field]
	if !ok {
		return fmt.Errorf("entity %q: states field %q is not a declared field", name, s.Field)
	}
	if field.Type != schema.Enum {
		return fmt.Errorf("entity %q: states field %q must be an Enum, not %s", name, s.Field, schema.FieldTypeLabel(field.Type, false))
	}
	if err := checkGuardedColumn(c, pk, "states field", s.Field); err != nil {
		return err
	}
	// A Default of any other type (a JSON `true`, a number) silently
	// bypasses the string checks below and would be written unchecked into
	// the enum column on create, so refuse it here, naming the field.
	if field.Default != nil {
		if _, ok := field.Default.(string); !ok {
			return fmt.Errorf("entity %q: states field %q has a non-string Default (%T); a state Default names one of the field's values and must be a string", name, s.Field, field.Default)
		}
	}
	inValues := func(what, v string) error {
		if !slices.Contains(field.Values, v) {
			return fmt.Errorf("entity %q: states %s %q is not one of %s's values %v", name, what, v, s.Field, field.Values)
		}
		return nil
	}
	for _, v := range s.Initial {
		if err := inValues("initial value", v); err != nil {
			return err
		}
	}
	initial := s.InitialValues(c.Fields)
	if len(initial) == 0 {
		return fmt.Errorf("entity %q: states lists no initial value and field %q has no Default; no create could set it", name, s.Field)
	}
	if d, ok := field.Default.(string); ok && d != "" && !slices.Contains(initial, d) {
		return fmt.Errorf("entity %q: field %q defaults to %q, which states initial %v does not hold", name, s.Field, d, initial)
	}
	seen := map[string]bool{}
	for i, t := range s.Transitions {
		where := fmt.Sprintf("transitions[%d]", i)
		if !transitionKeyGrammar.MatchString(t.Key) {
			return fmt.Errorf("entity %q: states %s key %q must be lowercase segments joined by single underscores (^[a-z][a-z0-9]*(_[a-z][a-z0-9]*)*$), each segment led by a letter, so no two keys normalize to one generated identifier", name, where, t.Key)
		}
		if surface, reserved := reservedTransitionKeys[t.Key]; reserved {
			return fmt.Errorf("entity %q: states %s key %q is reserved: it collides with %s; rename the move", name, where, t.Key, surface)
		}
		if seen[t.Key] {
			return fmt.Errorf("entity %q: states declares move %q more than once", name, t.Key)
		}
		seen[t.Key] = true
		if len(t.From) == 0 {
			return fmt.Errorf("entity %q: states move %q has no From; name the values it starts from", name, t.Key)
		}
		for j, v := range t.From {
			if err := inValues(fmt.Sprintf("move %q from", t.Key), v); err != nil {
				return err
			}
			if slices.Contains(t.From[:j], v) {
				return fmt.Errorf("entity %q: states move %q lists from value %q twice", name, t.Key, v)
			}
		}
		if err := inValues(fmt.Sprintf("move %q to", t.Key), t.To); err != nil {
			return err
		}
		if t.Variant != "" && !displayKeyGrammar.MatchString(t.Variant) {
			return fmt.Errorf("entity %q: states move %q variant %q must be a lowercase slug", name, t.Key, t.Variant)
		}
		if t.Stamp == "" {
			continue
		}
		stamp, ok := byName[t.Stamp]
		if !ok {
			return fmt.Errorf("entity %q: states move %q stamp %q is not a declared field", name, t.Key, t.Stamp)
		}
		if t.Stamp == s.Field {
			return fmt.Errorf("entity %q: states move %q stamps the state field itself", name, t.Key)
		}
		if stamp.Type != schema.Date && stamp.Type != schema.Timestamp {
			return fmt.Errorf("entity %q: states move %q stamp %q must be a Date or Timestamp field, not %s", name, t.Key, t.Stamp, schema.FieldTypeLabel(stamp.Type, false))
		}
		if stamp.Default != nil {
			return fmt.Errorf("entity %q: states move %q stamp %q has a Default; a create would set it", name, t.Key, t.Stamp)
		}
		if stamp.AutoGenerate != schema.AutoNone {
			return fmt.Errorf("entity %q: states move %q stamp %q is auto-generated; the move could not set it", name, t.Key, t.Stamp)
		}
		if err := checkGuardedColumn(c, pk, fmt.Sprintf("move %q stamp", t.Key), t.Stamp); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStates runs the states block's boot checks over an EntityConfig
// the way App.Entity would, for code generators that read hand-written
// entity declarations (entities/*.go through packReadEntities) which never
// passed registration. Define runs first so the guards judge the same
// field set the app judges — Define injects the framework-managed columns
// (timestamps, deleted_at, tenant and owner columns) the guards refuse.
// The states block is what it checks, but Define panics on other
// declaration errors (an undeclared SearchFields entry, a bad ReadScope
// predicate); those come back as the error too, so a generator reading a
// hand-edited spec fails naming the entity rather than with a stack trace.
func ValidateStates(name string, cfg EntityConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	e := Define(name, cfg)
	if e.Config.States == nil {
		return nil
	}
	return e.Config.States.validate(e.Config, e.PrimaryKey)
}

// checkGuardedColumn refuses a state field or stamp that is also a column
// the framework manages itself: the primary key, the owner and tenant
// scope columns, the soft-delete column, and every auto-generated column
// (the injected created_at/updated_at, or any field the declaration marks
// AutoGenerate). Writing any of them through a move would fight the
// machinery that already owns it — a Stamp of deleted_at would soft-delete
// the row, and delete/restore would then write a guarded column outside
// any move.
func checkGuardedColumn(c EntityConfig, pk, what, col string) error {
	if pk == "" {
		pk = "id"
	}
	if col == pk {
		return fmt.Errorf("entity %q: states %s %q is the primary key", c.Name, what, col)
	}
	if c.Scope != nil {
		if col == c.Scope.OwnerField {
			return fmt.Errorf("entity %q: states %s %q is the owner field", c.Name, what, col)
		}
		if c.Scope.MultiTenant && col == c.TenantColumn() {
			return fmt.Errorf("entity %q: states %s %q is the tenant column", c.Name, what, col)
		}
		// Soft delete writes deleted_at on delete and restore, and every
		// scoped read filters on it by name; the column is managed whether
		// the declaration declared it or Define injected it.
		if c.Scope.SoftDelete && col == "deleted_at" {
			return fmt.Errorf("entity %q: states %s %q is the soft-delete column; delete and restore write it, a move never may", c.Name, what, col)
		}
	}
	for _, f := range c.Fields {
		if f.Name == col && f.AutoGenerate != schema.AutoNone {
			return fmt.Errorf("entity %q: states %s %q is auto-generated; the framework writes it (created_at/updated_at when the entity has timestamps)", c.Name, what, col)
		}
	}
	return nil
}

// copyStatesConfig returns a deep copy of s; nil stays nil.
func copyStatesConfig(s *StatesConfig) *StatesConfig {
	if s == nil {
		return nil
	}
	out := *s
	out.Initial = slices.Clone(s.Initial)
	if s.Transitions != nil {
		out.Transitions = make([]Transition, len(s.Transitions))
		for i, t := range s.Transitions {
			t.From = slices.Clone(t.From)
			out.Transitions[i] = t
		}
	}
	return &out
}

// MarkAudited records that an audit log writes this entity's rows.
// framework.WithAuditLog calls it for each entity it records; a state
// override (crud.WithStateOverride) is refused on an entity it never
// marked, since the override's only trail is the audit row.
func (e *Entity) MarkAudited() { atomic.StoreInt32(&e.audited, 1) }

// Audited reports whether MarkAudited was called.
func (e *Entity) Audited() bool { return atomic.LoadInt32(&e.audited) == 1 }
