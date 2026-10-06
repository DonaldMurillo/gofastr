package entity

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	// and its MCP tool: a lowercase slug, unique on the entity.
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

// reservedTransitionKeys collide with the entity's own MCP tool names
// (<entity>_list, …) and so cannot name a move.
var reservedTransitionKeys = map[string]bool{"list": true, "get": true, "create": true, "update": true, "delete": true}

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
		if !displayKeyGrammar.MatchString(t.Key) {
			return fmt.Errorf("entity %q: states %s key %q must be a lowercase slug matching ^[a-z][a-z0-9_]*$", name, where, t.Key)
		}
		if reservedTransitionKeys[t.Key] {
			return fmt.Errorf("entity %q: states %s key %q is reserved (list, get, create, update and delete name the entity's own tools)", name, where, t.Key)
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

// checkGuardedColumn refuses a state field or stamp that is also a column
// the framework manages itself.
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
