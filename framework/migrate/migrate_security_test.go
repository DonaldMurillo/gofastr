package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Property: every value spliced into generated DDL is escaped for the
// literal context it lands in — for a DEFAULT clause that means a
// single-quoted string literal that cannot be closed from inside.
//
// Surfaces: SQLDefault feeds ColumnDefaultClause, which feeds BOTH
// columnDefs (CREATE TABLE) and diffEntityFromLive (ALTER TABLE ADD
// COLUMN). schema.Field.Default is `any`, and only the `case string`
// arm doubled quotes; the default: arm rendered fmt.Sprintf("'%v'", v)
// raw. A named string type, a fmt.Stringer, or — the reachable one — a
// []any / map[string]any decoded from JSON took that arm.
//
// Reachability: entity.Declaration's Default is `any` and kiln's
// add_entity op accepts a field default straight from an HTTP JSON
// payload, so a JSON array or object arrives as []any and closes the
// literal. A verified payload created and COMMITTED an extra column.
func TestSQLDefaultEscapesEveryType(t *testing.T) {
	type named string
	const payload = `x', shadow TEXT DEFAULT 'y`

	cases := []struct {
		name string
		val  any
	}{
		{"plain string", payload},
		{"named string type", named(payload)},
		{"json array", []any{payload}},
		{"json object", map[string]any{"a": payload}},
		{"stringer", stringerDefault(payload)},
	}

	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		for _, tc := range cases {
			got := SQLDefault(schema.Field{Default: tc.val}, dialect)
			if !isSingleQuotedLiteral(got) {
				t.Errorf("SECURITY: [migrate] SQLDefault(%s, %s) = %s — the literal is closable. "+
					"Attack: a kiln add_entity field default injects DDL that COMMITS "+
					"(an extra column, a dropped table) into CREATE TABLE / ALTER TABLE ADD COLUMN.",
					tc.name, dialect, got)
			}
		}
	}

	// Non-string scalars keep their unquoted native rendering.
	if got := SQLDefault(schema.Field{Default: 42}, DialectSQLite); got != "42" {
		t.Errorf("[migrate] int default regressed: %s", got)
	}
	if got := SQLDefault(schema.Field{Default: true}, DialectPostgres); got != "TRUE" {
		t.Errorf("[migrate] bool default regressed: %s", got)
	}
	if got := SQLDefault(schema.Field{Default: "o'brien"}, DialectSQLite); got != "'o''brien'" {
		t.Errorf("[migrate] string escaping regressed: %s", got)
	}
}

type stringerDefault string

func (s stringerDefault) String() string { return string(s) }

// isSingleQuotedLiteral reports whether s is one SQL string literal:
// wrapped in single quotes with every interior quote doubled, so the
// literal cannot be terminated early.
func isSingleQuotedLiteral(s string) bool {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return false
	}
	inner := s[1 : len(s)-1]
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\'' {
			continue
		}
		if i+1 >= len(inner) || inner[i+1] != '\'' {
			return false
		}
		i++ // skip the escaped pair
	}
	return true
}

// TestReadLiveColumnsRejectsBadTable pins that the SQLite live-column
// read validates its identifier like every one of its siblings. It is
// the only identifier site in the package that interpolated without
// SafeIdent, justified by a comment claiming the table name "is taken
// from our own registry, not user input" — which is false: kiln's
// add_entity / update_entity ops accept a `table` override over HTTP
// (kiln/journal/replay.go validates only the entity Name), and this
// read runs at AutoMigratePlanContext BEFORE any SafeIdent call.
//
// No injection landed today only because database/sql executes a single
// statement; that is the driver's property, not this package's.
func TestReadLiveColumnsRejectsBadTable(t *testing.T) {
	for _, bad := range []string{
		`foo); DROP TABLE victim; --`,
		`foo" ; DROP TABLE victim; --`,
		"foo\nbar",
	} {
		_, err := ReadLiveColumnsSQLite(context.Background(), nil, bad)
		if err == nil {
			t.Errorf("SECURITY: [migrate] ReadLiveColumnsSQLite accepted table name %q. "+
				"Attack: an agent-supplied table override reaches PRAGMA table_info "+
				"unvalidated, ahead of every SafeIdent call in the plan path.", bad)
			continue
		}
		if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "invalid") {
			t.Errorf("[migrate] unexpected rejection reason for %q: %v", bad, err)
		}
	}
}
