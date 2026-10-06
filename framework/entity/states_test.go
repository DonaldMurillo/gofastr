package entity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// statesFields is the invoices field set the states fixture is written
// against: the Enum state field with a Default, a Date stamp, and enough
// other columns to break individual guards against.
func statesFields() []schema.Field {
	return []schema.Field{
		{Name: "number", Type: schema.String, Required: true},
		{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid", "void"}},
		{Name: "amount", Type: schema.Decimal},
		{Name: "paid_on", Type: schema.Date},
	}
}

// statesFixture is a StatesConfig that passes every boot check; tests copy
// it and break one thing.
func statesFixture() *StatesConfig {
	return &StatesConfig{
		Field:   "status",
		Initial: []string{"draft", "open"},
		Transitions: []Transition{
			{Key: "issue", Label: "Issue", From: []string{"draft"}, To: "open"},
			{Key: "pay", Label: "Record payment", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
			{Key: "void", From: []string{"draft", "open", "paid"}, To: "void"},
			{Key: "mark_overdue", From: []string{"open"}, To: "void", System: true},
		},
	}
}

// statesEntityErr defines an entity carrying a mutated copy of the fixture
// and returns its Validate error.
func statesEntityErr(t *testing.T, mutate func(c *EntityConfig, s *StatesConfig)) error {
	t.Helper()
	fields := statesFields()
	st := statesFixture()
	cfg := EntityConfig{Name: "invoices", Table: "invoices", Fields: fields, States: st}
	if mutate != nil {
		mutate(&cfg, st)
	}
	return Define("invoices", cfg).Validate()
}

func TestStatesFixturePassesBootCheck(t *testing.T) {
	if err := statesEntityErr(t, nil); err != nil {
		t.Fatalf("fixture states rejected: %v", err)
	}
	// Nil states means no state machine and must stay nil after Define.
	e := Define("invoices", EntityConfig{Fields: statesFields()})
	if e.Config.States != nil {
		t.Fatalf("nil States must stay nil after Define, got %+v", e.Config.States)
	}
}

// Every boot refusal names the offender, so a typo fails the app at boot
// with something actionable rather than a surprise 500 on the first write.
func TestStatesBootRefusalsNameOffender(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(c *EntityConfig, s *StatesConfig)
		want   string
	}{
		{"missing field", func(_ *EntityConfig, s *StatesConfig) { s.Field = "" },
			`entity "invoices": states field is empty`},
		{"undeclared field", func(_ *EntityConfig, s *StatesConfig) { s.Field = "stage" },
			`states field "stage" is not a declared field`},
		{"non-Enum field", func(c *EntityConfig, _ *StatesConfig) {
			c.Fields[1].Type = schema.String
		}, `states field "status" must be an Enum`},
		{"initial not in values", func(_ *EntityConfig, s *StatesConfig) { s.Initial = []string{"draft", "settled"} },
			`states initial value "settled" is not one of status's values`},
		{"no initial and no default", func(c *EntityConfig, s *StatesConfig) {
			s.Initial = nil
			c.Fields[1].Default = nil
		}, `states lists no initial value and field "status" has no Default`},
		{"default outside initial", func(c *EntityConfig, _ *StatesConfig) {
			c.Fields[1].Default = "paid"
		}, `field "status" defaults to "paid", which states initial [draft open] does not hold`},
		{"bad key grammar", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "Issue" },
			`states transitions[0] key "Issue" must be lowercase segments joined by single underscores`},
		{"double underscore key", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "mark__paid" },
			`states transitions[0] key "mark__paid" must be lowercase segments joined by single underscores`},
		{"trailing underscore key", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "mark_paid_" },
			`states transitions[0] key "mark_paid_" must be lowercase segments joined by single underscores`},
		{"digit-led segment key", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "a_1" },
			`states transitions[0] key "a_1" must be lowercase segments joined by single underscores`},
		{"normalized pair mark_paid and mark__paid", func(_ *EntityConfig, s *StatesConfig) {
			s.Transitions[0].Key = "mark_paid"
			s.Transitions[1].Key = "mark__paid"
		}, `states transitions[1] key "mark__paid" must be lowercase segments joined by single underscores`},
		{"reserved key patch", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "patch" },
			`key "patch" is reserved: it collides with the entity's own OpenAPI id patch_<Schema> and client method Patch<Entity>`},
		{"reserved key transition", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "transition" },
			`key "transition" is reserved: it collides with the JS SDK resource's own transition method`},
		{"reserved key client", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "client" },
			`key "client" is reserved: it collides with the JS SDK resource's own client field`},
		{"reserved key table", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "table" },
			`key "table" is reserved: it collides with the JS SDK resource's own table field`},
		{"reserved key constructor", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "constructor" },
			`key "constructor" is reserved: it collides with the JS SDK resource's constructor`},
		{"reserved key events", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[0].Key = "events" },
			`key "events" is reserved: it collides with the entity's own OpenAPI id events_<Schema> (the SSE subscription)`},
		{"duplicate key", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].Key = "issue" },
			`states declares move "issue" more than once`},
		{"empty From", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].From = nil },
			`states move "pay" has no From`},
		{"From not in values", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].From = []string{"opened"} },
			`states move "pay" from "opened" is not one of status's values`},
		{"duplicate From value", func(_ *EntityConfig, s *StatesConfig) {
			s.Transitions[1].From = []string{"open", "open"}
		}, `states move "pay" lists from value "open" twice`},
		{"To not in values", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].To = "settled" },
			`states move "pay" to "settled" is not one of status's values`},
		{"bad variant", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].Variant = "Primary" },
			`states move "pay" variant "Primary" must be a lowercase slug`},
		{"stamp undeclared", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].Stamp = "settled_on" },
			`states move "pay" stamp "settled_on" is not a declared field`},
		{"stamp is the state field", func(_ *EntityConfig, s *StatesConfig) { s.Transitions[1].Stamp = "status" },
			`states move "pay" stamps the state field itself`},
		{"stamp wrong type", func(c *EntityConfig, s *StatesConfig) {
			c.Fields = append(c.Fields, schema.Field{Name: "note", Type: schema.String})
			s.Transitions[1].Stamp = "note"
		}, `states move "pay" stamp "note" must be a Date or Timestamp field`},
		{"stamp with Default", func(c *EntityConfig, s *StatesConfig) {
			c.Fields = append(c.Fields, schema.Field{Name: "cleared_on", Type: schema.Date, Default: "2026-01-01"})
			s.Transitions[1].Stamp = "cleared_on"
		}, `states move "pay" stamp "cleared_on" has a Default`},
		{"stamp auto-generated", func(c *EntityConfig, s *StatesConfig) {
			c.Fields = append(c.Fields, schema.Field{Name: "closed_at", Type: schema.Timestamp, AutoGenerate: schema.AutoTimestamp})
			s.Transitions[1].Stamp = "closed_at"
		}, `states move "pay" stamp "closed_at" is auto-generated`},
		{"stamp is the soft-delete column", func(c *EntityConfig, s *StatesConfig) {
			c.Scope = &ScopeConfig{SoftDelete: true}
			s.Transitions[1].Stamp = "deleted_at"
		}, `states move "pay" stamp "deleted_at" is the soft-delete column; delete and restore write it, a move never may`},
		{"state field is auto-generated", func(c *EntityConfig, _ *StatesConfig) {
			c.Fields[1].AutoGenerate = schema.AutoTimestamp
		}, `states field "status" is auto-generated`},
		{"state field is the owner field", func(c *EntityConfig, _ *StatesConfig) {
			c.Scope = &ScopeConfig{OwnerField: "status"}
		}, `states field "status" is the owner field`},
		{"state field is the tenant column", func(c *EntityConfig, _ *StatesConfig) {
			c.Scope = &ScopeConfig{MultiTenant: true, TenantField: "status"}
		}, `states field "status" is the tenant column`},
		{"non-string Default on the state field", func(c *EntityConfig, _ *StatesConfig) {
			c.Fields[1].Default = 42
		}, `field "status" has an invalid Default 42: must be a string`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := statesEntityErr(t, tc.mutate)
			if err == nil {
				t.Fatalf("invalid states accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the offender: %q", err, tc.want)
			}
		})
	}
}

// Define deep-copies the config: a caller that keeps a handle on its slices
// cannot change what the app validated at registration.
func TestStatesDeepCopiedByDefine(t *testing.T) {
	st := statesFixture()
	want := statesFixture()
	e := Define("invoices", EntityConfig{Fields: statesFields(), States: st})

	st.Initial[0] = "mutated"
	st.Transitions[1].From[0] = "mutated"
	st.Transitions[1].Stamp = "mutated"
	st.Transitions[1].To = "mutated"

	if !reflect.DeepEqual(e.Config.States, want) {
		t.Fatalf("mutating the caller's slices moved the entity's states:\n got %+v\nwant %+v", e.Config.States, want)
	}
}

// The states block decodes strictly: an unknown key anywhere in it is an
// error, and a valid block round-trips through EntityDeclaration into the
// EntityConfig Define consumes.
func TestStatesJSONStrictAndRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
	}{
		{"unknown key in states", `{"field":"status","bogus":true}`},
		{"unknown key in a transition", `{"field":"status","initial":["draft"],"transitions":[{"key":"pay","from":["open"],"to":"paid","lado":"x"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var st StatesConfig
			if err := json.Unmarshal([]byte(tc.json), &st); err == nil {
				t.Fatalf("unknown key accepted: %+v", st)
			}
		})
	}

	const decl = `{
		"name": "invoices",
		"table": "invoices",
		"fields": [
			{"name": "number", "type": "string", "required": true},
			{"name": "status", "type": "enum", "values": ["draft","open","paid","void"], "default": "draft"},
			{"name": "paid_on", "type": "date"}
		],
		"states": {
			"field": "status",
			"initial": ["draft", "open"],
			"transitions": [
				{"key": "issue", "label": "Issue", "from": ["draft"], "to": "open"},
				{"key": "pay", "from": ["open"], "to": "paid", "stamp": "paid_on", "permission": "invoices:pay"},
				{"key": "mark_overdue", "from": ["open"], "to": "void", "system": true}
			]
		}
	}`
	var d EntityDeclaration
	if err := json.Unmarshal([]byte(decl), &d); err != nil {
		t.Fatalf("declaration with a valid states block rejected: %v", err)
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("declaration Config: %v", err)
	}
	want := &StatesConfig{
		Field:   "status",
		Initial: []string{"draft", "open"},
		Transitions: []Transition{
			{Key: "issue", Label: "Issue", From: []string{"draft"}, To: "open"},
			{Key: "pay", From: []string{"open"}, To: "paid", Stamp: "paid_on", Permission: "invoices:pay"},
			{Key: "mark_overdue", From: []string{"open"}, To: "void", System: true},
		},
	}
	if !reflect.DeepEqual(cfg.States, want) {
		t.Fatalf("states block did not round-trip:\n got %+v\nwant %+v", cfg.States, want)
	}
}

// ValidateStates is the generators' entry point: it runs the states boot
// checks over a declaration that never passed registration, with Define's
// column injections applied, so the guards see the same field set the app
// sees. It refuses on its own what the entity-wide field loop would also
// catch at full boot (a non-string Default), and what only the states
// block knows (the soft-delete column as a stamp).
func TestValidateStatesRunsBootChecksOverDeclarations(t *testing.T) {
	if err := ValidateStates("invoices", EntityConfig{Name: "invoices", Table: "invoices", Fields: statesFields(), States: statesFixture()}); err != nil {
		t.Fatalf("ValidateStates rejected the fixture: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(c *EntityConfig, s *StatesConfig)
		want   string
	}{
		{"non-string Default", func(c *EntityConfig, _ *StatesConfig) {
			c.Fields[1].Default = 42
		}, `states field "status" has a non-string Default (int)`},
		{"soft-delete stamp", func(c *EntityConfig, s *StatesConfig) {
			c.Scope = &ScopeConfig{SoftDelete: true}
			s.Transitions[1].Stamp = "deleted_at"
		}, `states move "pay" stamp "deleted_at" is the soft-delete column`},
		{"reserved key remove", func(_ *EntityConfig, s *StatesConfig) {
			s.Transitions[0].Key = "remove"
		}, `key "remove" is reserved: it collides with the JS SDK resource's own remove member`},
		{"valid fixture row", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := statesFields()
			st := statesFixture()
			cfg := EntityConfig{Name: "invoices", Table: "invoices", Fields: fields, States: st}
			if tc.mutate != nil {
				tc.mutate(&cfg, st)
			}
			err := ValidateStates("invoices", cfg)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateStates rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateStates error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	// nil states means no state machine: nothing to validate, no panic.
	if err := ValidateStates("invoices", EntityConfig{Name: "invoices", Table: "invoices", Fields: statesFields()}); err != nil {
		t.Fatalf("ValidateStates rejected nil states: %v", err)
	}
}
