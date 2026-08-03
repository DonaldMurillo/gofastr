package entity

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func validateFields(t *testing.T, fields ...schema.Field) error {
	t.Helper()
	e := Define("things", EntityConfig{Fields: fields})
	return e.Validate()
}

// A Default the same validator would reject coming from a client must be
// rejected at registration. Issue #174: the create path validated the request
// body and only afterwards substituted Defaults, so a malformed Default
// reached the driver unchecked.
func TestValidateRejectsMalformedDefaults(t *testing.T) {
	cases := []struct {
		name  string
		field schema.Field
		want  string // substring the error must contain, beyond the field name
	}{
		{
			// The issue's own case: JSONB rejects `draft` on Postgres, SQLite
			// stores it in a TEXT column and reads it back unchanged.
			name:  "json default that is not JSON",
			field: schema.Field{Name: "flags", Type: schema.JSON, Default: "draft"},
			want:  "must be valid JSON",
		},
		{
			name:  "enum default outside Values",
			field: schema.Field{Name: "flags", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "archived"},
			want:  "must be one of",
		},
		{
			// ValidateAll treats a Required field as satisfied when Default is
			// non-nil, so "" passes request validation and then writes "".
			name:  "required string defaulted to empty",
			field: schema.Field{Name: "flags", Type: schema.String, Required: true, Default: ""},
			want:  "is required",
		},
		{
			name:  "int default out of range",
			field: schema.Field{Name: "flags", Type: schema.Int, Max: floatPtr(10), Default: 99},
			want:  "must be at most",
		},
		{
			name:  "string default violating Pattern",
			field: schema.Field{Name: "flags", Type: schema.String, Pattern: "^[a-z]+$", Default: "NOPE"},
			want:  "must match pattern",
		},
		{
			name:  "timestamp default that is not RFC 3339",
			field: schema.Field{Name: "flags", Type: schema.Timestamp, Default: "yesterday"},
			want:  "must be a valid RFC 3339 timestamp",
		},
		{
			name:  "uuid default that is not a uuid",
			field: schema.Field{Name: "flags", Type: schema.UUID, Default: "nope"},
			want:  "must be a valid UUID",
		},
		{
			name:  "decimal default that is not a number",
			field: schema.Field{Name: "flags", Type: schema.Decimal, Default: "abc"},
			want:  "must be a valid decimal number",
		},
		{
			name:  "decimal default below Min",
			field: schema.Field{Name: "flags", Type: schema.Decimal, Min: floatPtr(0), Default: -1},
			want:  "must be at least",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateFields(t, c.field)
			if err == nil {
				t.Fatalf("Validate() accepted field %q with Default %#v (%s) — the create path substitutes it for an omitted field AFTER ValidateAll, so it reaches the driver unchecked: a 500 with nothing actionable in it, or silently wrong data on a dialect whose column type is looser",
					c.field.Name, c.field.Default, c.want)
			}
			msg := err.Error()
			if !strings.Contains(msg, `"flags"`) {
				t.Errorf("error must name the field: %v", msg)
			}
			if !strings.Contains(msg, c.want) {
				t.Errorf("error = %q, want substring %q", msg, c.want)
			}
			if !strings.Contains(msg, "Default") {
				t.Errorf("error must say the problem is the Default, got %q", msg)
			}
		})
	}
}

// Every Default shape that works today must keep working — a false refusal at
// boot is worse than the bug it guards, because it breaks every caller at once.
func TestValidateAcceptsWorkingDefaults(t *testing.T) {
	cases := []struct {
		name  string
		field schema.Field
	}{
		{"enum in Values", schema.Field{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"}},
		{"string", schema.Field{Name: "status", Type: schema.String, Default: "draft"}},
		{"int zero on a required field", schema.Field{Name: "stock", Type: schema.Int, Required: true, Min: floatPtr(0), Default: 0}},
		{"bool literal", schema.Field{Name: "active", Type: schema.Bool, Default: true}},
		// battery/auth declares its Bool columns this way.
		{"bool as string", schema.Field{Name: "active", Type: schema.Bool, Default: "false"}},
		{"float", schema.Field{Name: "ratio", Type: schema.Float, Min: floatPtr(0), Default: 0}},
		// examples/meridian spells its Decimal defaults as wire-form strings…
		{"decimal as string", schema.Field{Name: "mrr", Type: schema.Decimal, Min: floatPtr(0), Default: "0"}},
		// …examples/ecommerce and every `decimal` blueprint field spell them
		// as Go numbers. Both reach the driver and both render valid DDL.
		{"decimal as int", schema.Field{Name: "tax", Type: schema.Decimal, Min: floatPtr(0), Default: 0}},
		{"decimal as float", schema.Field{Name: "tax", Type: schema.Decimal, Min: floatPtr(0), Default: 1.5}},
		// #170: a Go map/slice Default round-trips through marshalJSONColumn.
		{"json as map", schema.Field{Name: "flags", Type: schema.JSON, Default: map[string]any{"a": 1}}},
		{"json as slice", schema.Field{Name: "flags", Type: schema.JSON, Default: []any{"a"}}},
		{"json as JSON text", schema.Field{Name: "flags", Type: schema.JSON, Default: `{"a":1}`}},
		{"timestamp as RFC 3339", schema.Field{Name: "at", Type: schema.Timestamp, Default: "2026-01-02T15:04:05Z"}},
		{"timestamp as time.Time", schema.Field{Name: "at", Type: schema.Timestamp, Default: time.Unix(0, 0).UTC()}},
		{"date as time.Time", schema.Field{Name: "on", Type: schema.Date, Default: time.Unix(0, 0).UTC()}},
		{"text", schema.Field{Name: "body", Type: schema.Text, Default: ""}},
		{"relation", schema.Field{Name: "author_id", Type: schema.Relation, To: "users", Default: "sys"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateFields(t, c.field); err != nil {
				t.Fatalf("Validate() = %v, want nil — this Default works today", err)
			}
		})
	}
}

// Auto-generated fields never take their Default as an insert value: doCreate
// overwrites the body slot with the generated value. The Default survives only
// as the column's DDL DEFAULT, so it is not validated as a field value —
// matching schema.ValidateAll, which skips these fields too.
func TestValidateSkipsAutoGeneratedDefaults(t *testing.T) {
	err := validateFields(t, schema.Field{
		Name:         "code",
		Type:         schema.UUID,
		AutoGenerate: schema.AutoUUID,
		Default:      "not-a-uuid",
	})
	if err != nil {
		t.Fatalf("Validate() = %v, want nil (auto-generated field)", err)
	}
}

func floatPtr(f float64) *float64 { return &f }
