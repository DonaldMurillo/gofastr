package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// statusDefault returns the status field's Default in e's fields.
func statusDefault(t *testing.T, fields []schema.Field) any {
	t.Helper()
	for _, f := range fields {
		if f.Name == "status" {
			return f.Default
		}
	}
	t.Fatal("no status field")
	return nil
}

// Define gives a state field with no Default the first Initial value, so
// a create that leaves the state out starts where a move can pick it up
// (2026-10-27 bug bash D7). The caller's field slice is not changed, and a
// declared Default is kept.
func TestDefineFillsStateDefault(t *testing.T) {
	for _, tc := range []struct {
		name    string
		declare any
		want    any
	}{
		{"none", nil, "draft"},
		{"empty", "", "draft"},
		{"declared", "open", "open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := statesFields()
			fields[1].Default = tc.declare
			e := Define("invoices", EntityConfig{Fields: fields, States: statesFixture()})
			if got := statusDefault(t, e.Config.Fields); got != tc.want {
				t.Fatalf("status Default = %#v, want %#v", got, tc.want)
			}
			if got := statusDefault(t, fields); got != tc.declare {
				t.Fatalf("Define changed the caller's field slice: status Default = %#v, want %#v", got, tc.declare)
			}
			if err := e.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}
