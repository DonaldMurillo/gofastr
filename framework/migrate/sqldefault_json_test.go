package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A schema.JSON field's Default is naturally authored as a Go map or slice,
// that is the shape the write path handles (crud.marshalJSONColumn) and the
// shape schema.validateJSON accepts. SQLDefault had no arm for either, so both
// fell to the fmt.Sprintf("%v") fallback and rendered as Go's debug form:
// DEFAULT 'map[a:1]' on a JSONB column, which Postgres rejects at AutoMigrate.
//
// The same declaration works on SQLite, whose column is TEXT, so the value is
// right in two places (validation, insert) and wrong only in the DDL.
func TestSQLDefaultRendersJSONForMapsAndSlices(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field schema.Field
		want  string
	}{
		{"map", schema.Field{Name: "flags", Type: schema.JSON, Default: map[string]any{"a": 1}}, `'{"a":1}'`},
		{"slice", schema.Field{Name: "tags", Type: schema.JSON, Default: []any{"x", "y"}}, `'["x","y"]'`},
		{"empty map", schema.Field{Name: "cfg", Type: schema.JSON, Default: map[string]any{}}, `'{}'`},
		{"empty slice", schema.Field{Name: "list", Type: schema.JSON, Default: []any{}}, `'[]'`},
		// A string default is already JSON on the wire and must be untouched,
		// or the fix would double-encode every declaration that works today.
		{"string passthrough", schema.Field{Name: "ok", Type: schema.JSON, Default: `{"a":1}`}, `'{"a":1}'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SQLDefault(tc.field, DialectPostgres)
			if got != tc.want {
				t.Errorf("SQLDefault(%s) = %s, want %s — Postgres rejects this on a JSONB column at AutoMigrate",
					tc.name, got, tc.want)
			}
			if strings.Contains(got, "map[") || strings.Contains(got, "[x y]") {
				t.Errorf("SQLDefault(%s) emitted Go's debug rendering: %s", tc.name, got)
			}
		})
	}
}

// The escaping in the default arm is deliberate and documented: a kiln
// add_entity payload once committed an extra column through the old unescaped
// fmt.Sprintf("'%v'", v) fallback. Marshalling must happen BEFORE quoting, not
// instead of it, so a value carrying a quote still cannot terminate the
// literal.
func TestSQLDefaultJSONCannotEscapeItsLiteral(t *testing.T) {
	f := schema.Field{Name: "x", Type: schema.JSON, Default: map[string]any{"k": `'); DROP TABLE things; --`}}
	got := SQLDefault(f, DialectPostgres)
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Fatalf("not a quoted literal: %s", got)
	}
	// Every interior quote must be doubled; an odd count means one of them
	// closed the literal and the rest is loose DDL.
	if strings.Count(got, "'")%2 != 0 {
		t.Errorf("unbalanced quotes — the literal can be terminated from inside: %s", got)
	}
	if strings.Contains(got, "DROP TABLE things; --'") && !strings.Contains(got, "''") {
		t.Errorf("interior quote was not doubled: %s", got)
	}
}
