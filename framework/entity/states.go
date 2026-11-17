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

// StatesConfig names an entity's Enum state field and the moves that change it.
// Unless Advisory, the field and the stamps change only through a move.
type StatesConfig struct {
	// Field is the Enum field holding the state.
	Field string `json:"field"`
	// Initial lists the values a create may set; empty means the field's Default.
	Initial []string `json:"initial,omitempty"`
	// Transitions are the moves, in the order screens draw them.
	Transitions []Transition `json:"transitions,omitempty"`
	// Advisory lets direct writes change the state field and the stamps.
	Advisory bool `json:"advisory,omitempty"`
}

func transitionWrites(s *StatesConfig, c string) bool {
	return s != nil && (s.Field == c || slices.ContainsFunc(s.Transitions, func(t Transition) bool { return t.Stamp == c }))
}

// Transition is one named move of the state field.
type Transition struct {
	// Key names the move in its route and MCP tool: lowercase segments joined by
	// single underscores, unique on the entity and not a reserved name.
	Key string `json:"key"`
	// Label is the button text; empty draws the key.
	Label string `json:"label,omitempty"`
	// From lists the values the move starts from; never empty.
	From []string `json:"from"`
	// To is the value the move writes.
	To string `json:"to"`
	// Stamp names a Date or Timestamp field the move sets to the server's UTC now.
	Stamp string `json:"stamp,omitempty"`
	// Variant is the button variant (ui.ParseButtonVariant); empty is the default.
	Variant string `json:"variant,omitempty"`
	// Permission is required on top of update access; empty means update access alone.
	Permission string `json:"permission,omitempty"`
	// System moves get no route, button or MCP tool; only Go code runs them.
	System bool `json:"system,omitempty"`
}

// UnmarshalJSON decodes strictly: an unknown key is an error.
func (s *StatesConfig) UnmarshalJSON(data []byte) error {
	type plain StatesConfig
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

// Open returns the keys of the moves whose From holds value, in declared order, System moves included.
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

// Guarded returns the state field then each distinct stamp field.
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

func (s *StatesConfig) withStateDefault(fields []schema.Field) []schema.Field {
	if s == nil || len(s.Initial) == 0 {
		return fields
	}
	for i, f := range fields {
		if f.Name != s.Field {
			continue
		}
		if f.Default != nil && f.Default != "" {
			return fields
		}
		out := slices.Clone(fields)
		out[i].Default = s.Initial[0]
		return out
	}
	return fields
}

// InitialValues returns Initial, or the field's Default when Initial is empty.
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

var transitionKeyGrammar = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z][a-z0-9]*)*$`)

// reservedTransitionKeys maps each key a generated surface already uses to the surface, for the boot error.
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
	"columns":      "the JS SDK resource's own columns field",
	"constructor":  "the JS SDK resource's constructor",
}

func (s *StatesConfig) validate(c EntityConfig) error {
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
	if err := checkGuardedColumn(c, "states field", s.Field); err != nil {
		return err
	}
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
		if err := checkGuardedColumn(c, fmt.Sprintf("move %q stamp", t.Key), t.Stamp); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStates runs the states boot checks over cfg after Define, returning a Define panic as the error.
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
	return e.Config.States.validate(e.Config)
}

// ValidateDisplay is ValidateStates for the display block, query strings aside.
func ValidateDisplay(name string, cfg EntityConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	e := Define(name, cfg)
	if e.Config.Display == nil {
		return nil
	}
	return e.Config.Display.validate(e.Config.Name, e.Config.Fields, e.Config.Pagination)
}

func checkGuardedColumn(c EntityConfig, what, col string) error {
	if slices.Contains(c.keyColumns(), col) {
		return fmt.Errorf("entity %q: states %s %q is the primary key", c.Name, what, col)
	}
	if c.Scope != nil {
		if col == c.Scope.OwnerField {
			return fmt.Errorf("entity %q: states %s %q is the owner field", c.Name, what, col)
		}
		if c.Scope.MultiTenant && col == c.TenantColumn() {
			return fmt.Errorf("entity %q: states %s %q is the tenant column", c.Name, what, col)
		}
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
func (e *Entity) MarkAudited() { atomic.StoreInt32(&e.audited, 1) }

// Audited reports whether MarkAudited was called.
func (e *Entity) Audited() bool { return atomic.LoadInt32(&e.audited) == 1 }
