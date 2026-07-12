package filter

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func predFields() []schema.Field {
	return []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "priority", Type: schema.String},
		{Name: "assignee", Type: schema.String},
		{Name: "score", Type: schema.Int},
		{Name: "secret", Type: schema.String, Hidden: true},
	}
}

func mustParse(t *testing.T, raw string) *Predicate {
	t.Helper()
	p, err := ParseWhere(raw, predFields())
	if err != nil {
		t.Fatalf("ParseWhere(%s): %v", raw, err)
	}
	return p
}

func TestWhereEmptyIsNil(t *testing.T) {
	p, err := ParseWhere("", predFields())
	if err != nil || p != nil {
		t.Fatalf("empty where: got (%v, %v), want (nil, nil)", p, err)
	}
}

func TestWhereFlatLeaf(t *testing.T) {
	p := mustParse(t, `{"field":"status","op":"eq","value":"open"}`)
	c := BuildPredicate(p)
	if c.SQL != `(status = $1)` {
		t.Fatalf("sql = %q", c.SQL)
	}
	if len(c.Args) != 1 || c.Args[0] != "open" {
		t.Fatalf("args = %v", c.Args)
	}
}

func TestWhereNestedOrAnd(t *testing.T) {
	// status = A OR (priority = high AND assignee = me)
	raw := `{"or":[{"field":"status","value":"A"},{"and":[{"field":"priority","value":"high"},{"field":"assignee","value":"me"}]}]}`
	c := BuildPredicate(mustParse(t, raw))
	want := `((status = $1) OR ((priority = $2) AND (assignee = $3)))`
	if c.SQL != want {
		t.Fatalf("sql = %q\nwant %q", c.SQL, want)
	}
	if len(c.Args) != 3 || c.Args[0] != "A" || c.Args[1] != "high" || c.Args[2] != "me" {
		t.Fatalf("args = %v (order must match placeholder order)", c.Args)
	}
}

func TestWhereDefaultOpIsEq(t *testing.T) {
	c := BuildPredicate(mustParse(t, `{"field":"status","value":"x"}`))
	if !strings.Contains(c.SQL, "status = $1") {
		t.Fatalf("default op should be eq: %q", c.SQL)
	}
}

func TestWhereLikeEscapes(t *testing.T) {
	c := BuildPredicate(mustParse(t, `{"field":"status","op":"like","value":"50%"}`))
	if !strings.Contains(c.SQL, `LIKE $1 ESCAPE '\'`) {
		t.Fatalf("like must carry ESCAPE: %q", c.SQL)
	}
	// The % metachar must be escaped in the bound arg, never left raw.
	if got := c.Args[0].(string); !strings.Contains(got, `\%`) {
		t.Fatalf("like arg not escaped: %q", got)
	}
}

func TestWhereInParameterized(t *testing.T) {
	c := BuildPredicate(mustParse(t, `{"field":"status","op":"in","values":["a","b","c"]}`))
	if c.SQL != `(status IN ($1,$2,$3))` {
		t.Fatalf("sql = %q", c.SQL)
	}
	if len(c.Args) != 3 {
		t.Fatalf("args = %v", c.Args)
	}
}

// --- Security: field whitelist ---

func TestWhereUnknownFieldRejected(t *testing.T) {
	if _, err := ParseWhere(`{"field":"nope","value":"x"}`, predFields()); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}

func TestWhereHiddenFieldRejected(t *testing.T) {
	// A Hidden field must be indistinguishable from unknown — no predicate,
	// or it becomes a value-disclosure oracle.
	if _, err := ParseWhere(`{"field":"secret","op":"like","value":"a"}`, predFields()); err == nil {
		t.Fatal("Hidden field must be rejected in a where tree")
	}
}

func TestWhereUnknownOperatorRejected(t *testing.T) {
	if _, err := ParseWhere(`{"field":"status","op":"regex","value":"x"}`, predFields()); err == nil {
		t.Fatal("unknown operator must be rejected")
	}
}

// --- Security: injection lands in args, never the SQL string ---

func TestWhereInjectionPayloadIsBound(t *testing.T) {
	payload := `x'; DROP TABLE users;--`
	c := BuildPredicate(mustParse(t, `{"field":"status","value":"`+payload+`"}`))
	if strings.Contains(c.SQL, "DROP") || strings.Contains(c.SQL, payload) {
		t.Fatalf("SECURITY: payload leaked into SQL string: %q", c.SQL)
	}
	if c.Args[0] != payload {
		t.Fatalf("payload must be a bound arg verbatim, got %v", c.Args)
	}
	// The SQL contains only the whitelisted field name + placeholder.
	if c.SQL != `(status = $1)` {
		t.Fatalf("sql = %q", c.SQL)
	}
}

func TestWhereFieldNameCannotInject(t *testing.T) {
	// A field name with SQL metacharacters is not in the schema whitelist,
	// so it can never reach the SQL string.
	if _, err := ParseWhere(`{"field":"status; DROP TABLE t","value":"x"}`, predFields()); err == nil {
		t.Fatal("SECURITY: metachar field name must be rejected by the whitelist")
	}
}

// --- Security: DoS bounds, fail-closed ---

func TestWhereDepthBounded(t *testing.T) {
	// Build a tree deeper than maxPredicateDepth.
	raw := `{"field":"status","value":"x"}`
	for i := 0; i < maxPredicateDepth+2; i++ {
		raw = `{"and":[` + raw + `]}`
	}
	if _, err := ParseWhere(raw, predFields()); err == nil {
		t.Fatal("SECURITY: over-deep tree must be rejected")
	}
}

func TestWhereNodeCountBounded(t *testing.T) {
	var kids []string
	for i := 0; i < maxPredicateNodes+5; i++ {
		kids = append(kids, `{"field":"status","value":"x"}`)
	}
	raw := `{"and":[` + strings.Join(kids, ",") + `]}`
	if _, err := ParseWhere(raw, predFields()); err == nil {
		t.Fatal("SECURITY: over-wide tree must be rejected")
	}
}

func TestWhereAmbiguousNodeRejected(t *testing.T) {
	// A node that is both a group and a leaf is malformed.
	if _, err := ParseWhere(`{"field":"status","value":"x","or":[{"field":"status","value":"y"}]}`, predFields()); err == nil {
		t.Fatal("ambiguous node must be rejected")
	}
}

func TestWhereMalformedJSONRejected(t *testing.T) {
	if _, err := ParseWhere(`{"field":`, predFields()); err == nil {
		t.Fatal("malformed JSON must be rejected")
	}
}
