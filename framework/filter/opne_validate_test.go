package filter

import (
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// --- OpNe wiring: suffix table, where map, SQL builder ---

func TestOpNeQuerySuffixParses(t *testing.T) {
	q := url.Values{"status_ne": {"archived"}}
	fs, err := ParseFiltersValues(q, predFields())
	if err != nil {
		t.Fatalf("ParseFiltersValues: %v", err)
	}
	if len(fs) != 1 || fs[0].Op != OpNe || fs[0].Field != "status" || fs[0].Value != "archived" {
		t.Fatalf("filters = %+v, want one status OpNe filter", fs)
	}
}

func TestOpNeFilterSuffixTableEntry(t *testing.T) {
	for _, s := range FilterSuffixes {
		if s.Suffix == "_ne" && s.Op == OpNe {
			return
		}
	}
	t.Fatal("FilterSuffixes must carry {\"_ne\", OpNe} or the nested-filter surface silently maps _ne to a wrong op")
}

func TestOpNeBuildsNotEqualSQL(t *testing.T) {
	c := BuildPredicate(mustParse(t, `{"field":"status","op":"ne","value":"archived"}`))
	if c.SQL != `(status != $1)` {
		t.Fatalf("sql = %q, want (status != $1)", c.SQL)
	}
	if len(c.Args) != 1 || c.Args[0] != "archived" {
		t.Fatalf("args = %v", c.Args)
	}
}

func TestOpNeAppliesToQueryBuilder(t *testing.T) {
	q := url.Values{"status_ne": {"archived"}}
	fs, err := ParseFiltersValues(q, predFields())
	if err != nil {
		t.Fatal(err)
	}
	cb := query.Count("t")
	applyFilters(cb, fs)
	sql, _ := cb.Build()
	if !strings.Contains(sql, "status != $") {
		t.Fatalf("applyFilters did not render != for OpNe: %q", sql)
	}
}

func TestOpNeBoolCoerced(t *testing.T) {
	c := BuildPredicate(mustParseWith(t, vpFields(), `{"field":"active","op":"ne","value":"true"}`))
	if len(c.Args) != 1 {
		t.Fatalf("args = %v", c.Args)
	}
	if b, ok := c.Args[0].(bool); !ok || !b {
		t.Fatalf("bool leaf must bind a Go bool, got %#v", c.Args[0])
	}
}

// NULL semantics mirror OpEq: no IS NULL arm is bolted on, a NULL column
// matches neither = nor !=.
func TestOpNeNoNullArm(t *testing.T) {
	c := BuildPredicate(mustParse(t, `{"field":"status","op":"ne","value":"x"}`))
	if strings.Contains(strings.ToUpper(c.SQL), "IS NULL") {
		t.Fatalf("OpNe must not add an IS NULL arm: %q", c.SQL)
	}
}

// --- Operator/type fit, enforced by ParseWhere ---

func TestWhereLikeOnIntRefused(t *testing.T) {
	if _, err := ParseWhere(`{"field":"score","op":"like","value":"1"}`, predFields()); err == nil {
		t.Fatal("like on an Int column must be refused")
	}
}

func TestWhereLikeOnBoolRefused(t *testing.T) {
	fields := append(predFields(), schema.Field{Name: "active", Type: schema.Bool})
	if _, err := ParseWhere(`{"field":"active","op":"like","value":"tru"}`, fields); err == nil {
		t.Fatal("like on a Bool column must be refused")
	}
}

func TestWhereComparisonOnBoolRefused(t *testing.T) {
	fields := append(predFields(), schema.Field{Name: "active", Type: schema.Bool})
	if _, err := ParseWhere(`{"field":"active","op":"gt","value":"false"}`, fields); err == nil {
		t.Fatal("gt on a Bool column must be refused")
	}
}

// A JSON blob has no order a request value can compare against: on
// Postgres the JSONB cast of a plain string fails at query time.
func TestWhereRangeOnJSONRefused(t *testing.T) {
	fields := append(predFields(), schema.Field{Name: "payload", Type: schema.JSON})
	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		if _, err := ParseWhere(`{"field":"payload","op":"`+op+`","value":"1"}`, fields); err == nil {
			t.Errorf("%s on a JSON column must be refused", op)
		}
	}
}

func TestWhereNeSuitsEveryType(t *testing.T) {
	for _, f := range []schema.Field{
		{Name: "s", Type: schema.String}, {Name: "i", Type: schema.Int},
		{Name: "b", Type: schema.Bool}, {Name: "d", Type: schema.Date},
	} {
		if _, err := ParseWhere(`{"field":"`+f.Name+`","op":"ne","value":"x"}`, []schema.Field{f}); err != nil {
			t.Errorf("ne on %s column refused: %v", f.Name, err)
		}
	}
}

// --- ValidatePredicate: the checks a Go-built tree passes ---

func vpFields() []schema.Field {
	return []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "score", Type: schema.Int},
		{Name: "active", Type: schema.Bool},
		{Name: "due_on", Type: schema.Date, WireName: "dueDate"},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "card", Type: schema.String, NoQuery: true},
	}
}

func TestValidatePredicateNilOK(t *testing.T) {
	p, err := ValidatePredicate(nil, vpFields())
	if err != nil {
		t.Fatalf("nil predicate must be valid, got %v", err)
	}
	if p != nil {
		t.Fatalf("nil in, nil out; got %#v", p)
	}
}

func TestValidatePredicateAcceptsValidTree(t *testing.T) {
	p := &Predicate{Or: true, Children: []Predicate{
		{Field: "status", Op: OpEq, Value: "open"},
		{Children: []Predicate{
			{Field: "score", Op: OpGte, Value: "10"},
			{Field: "status", Op: OpIn, Values: []string{"a", "b"}},
		}},
	}}
	if _, err := ValidatePredicate(p, vpFields()); err != nil {
		t.Fatalf("valid tree refused: %v", err)
	}
}

func TestValidatePredicateSetsBoolMarker(t *testing.T) {
	p, err := ValidatePredicate(&Predicate{Field: "active", Op: OpEq, Value: "true"}, vpFields())
	if err != nil {
		t.Fatal(err)
	}
	if !p.isBool {
		t.Fatal("Bool leaf must get its coercion marker set so BuildPredicate binds a Go bool")
	}
	c := BuildPredicate(p)
	if b, ok := c.Args[0].(bool); !ok || !b {
		t.Fatalf("arg = %#v, want Go true", c.Args[0])
	}
	// And the schema is the authority: a wrongly hand-set marker on a
	// non-Bool column is cleared.
	q, err := ValidatePredicate(&Predicate{Field: "status", Op: OpEq, Value: "x", isBool: true}, vpFields())
	if err != nil {
		t.Fatal(err)
	}
	if q.isBool {
		t.Fatal("isBool on a String column must be reset by validation")
	}
}

func TestValidatePredicateResolvesWireAlias(t *testing.T) {
	p, err := ValidatePredicate(&Predicate{Field: "dueDate", Op: OpEq, Value: "2026-10-01"}, vpFields())
	if err != nil {
		t.Fatalf("wire alias refused: %v", err)
	}
	if p.Field != "due_on" {
		t.Fatalf("alias must resolve to the column, got %q", p.Field)
	}
}

func TestValidatePredicateRefusesMetaCharsField(t *testing.T) {
	if _, err := ValidatePredicate(&Predicate{Field: "id; DROP TABLE x; --", Op: OpEq, Value: "1"}, vpFields()); err == nil {
		t.Fatal("a field name with SQL metacharacters must be refused before BuildPredicate splices it into SQL")
	}
}

func TestValidatePredicateRefusesUnknownField(t *testing.T) {
	if _, err := ValidatePredicate(&Predicate{Field: "nope", Op: OpEq, Value: "x"}, vpFields()); err == nil {
		t.Fatal("unknown field must be refused")
	}
}

func TestValidatePredicateRefusesHiddenField(t *testing.T) {
	if _, err := ValidatePredicate(&Predicate{Field: "secret", Op: OpEq, Value: "x"}, vpFields()); err == nil {
		t.Fatal("Hidden field must be refused")
	}
}

func TestValidatePredicateRefusesNoQueryField(t *testing.T) {
	_, err := ValidatePredicate(&Predicate{Field: "card", Op: OpEq, Value: "4111"}, vpFields())
	if err == nil {
		t.Fatal("NoQuery field must be refused")
	}
	// By name, not as "unknown": the field is visible in responses, so
	// the only useful thing the refusal can add is that the query
	// surface refuses it (the documented NoQuery contract).
	if !strings.Contains(err.Error(), "cannot be filtered") {
		t.Fatalf("NoQuery refusal must name the field as not filterable, got: %v", err)
	}
}

func TestValidatePredicateRefusesLikeOnNonText(t *testing.T) {
	if _, err := ValidatePredicate(&Predicate{Field: "score", Op: OpLike, Value: "1"}, vpFields()); err == nil {
		t.Fatal("like on an Int column must be refused")
	}
}

func TestValidatePredicateRefusesEmptyIn(t *testing.T) {
	if _, err := ValidatePredicate(&Predicate{Field: "status", Op: OpIn}, vpFields()); err == nil {
		t.Fatal("OpIn with zero values must be refused")
	}
}

// Validation resolves the tree on a copy: the caller's input stays
// byte-for-byte what it built, both for a wire alias (which must not be
// rewritten into the input's Field) and for the Bool coercion marker.
// A host may share one parsed tree across goroutines, so a write here
// would be a data race against every other reader.
func TestValidatePredicateLeavesInputUntouched(t *testing.T) {
	in := &Predicate{Or: true, Children: []Predicate{
		{Field: "dueDate", Op: OpEq, Value: "2026-10-01"},
		{Field: "active", Op: OpEq, Value: "true", Values: []string{"x"}},
	}}
	resolved, err := ValidatePredicate(in, vpFields())
	if err != nil {
		t.Fatal(err)
	}
	if in.Field != "" || in.Or != true || len(in.Children) != 2 {
		t.Fatalf("group node rewritten: %#v", in)
	}
	if in.Children[0].Field != "dueDate" {
		t.Fatalf("input leaf's alias was resolved in place: %q", in.Children[0].Field)
	}
	if in.Children[1].isBool {
		t.Fatal("input leaf got the Bool coercion marker in place")
	}
	if resolved.Children[0].Field != "due_on" {
		t.Fatalf("resolved copy did not resolve the alias: %q", resolved.Children[0].Field)
	}
	if !resolved.Children[1].isBool {
		t.Fatal("resolved copy did not set the Bool coercion marker")
	}
	// The copy owns its Values slice: appending to it cannot reach the
	// input's backing array.
	resolved.Children[1].Values = append(resolved.Children[1].Values, "y")
	if len(in.Children[1].Values) != 1 {
		t.Fatal("resolved copy shares its Values backing array with the input")
	}
}

// A shared tree (a parsed Display view handed to concurrent requests)
// is validated and compiled from several goroutines at once. Validation
// must take no write on the shared tree, or this test races under
// -race (it did when validateLeaf resolved aliases in place).
func TestValidatePredicateSharedTreeNoRace(t *testing.T) {
	shared := &Predicate{Or: true, Children: []Predicate{
		{Field: "status", Op: OpEq, Value: "open"},
		{Field: "dueDate", Op: OpGte, Value: "2026-10-01"},
		{Field: "active", Op: OpEq, Value: "true"},
	}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				resolved, err := ValidatePredicate(shared, vpFields())
				if err != nil {
					t.Errorf("shared tree refused: %v", err)
					return
				}
				if c := BuildPredicate(resolved); c.SQL == "" {
					t.Error("resolved tree built no SQL")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestValidatePredicateRefusesEmptyGroup(t *testing.T) {
	if _, err := ValidatePredicate(&Predicate{Or: true, Children: []Predicate{}}, vpFields()); err == nil {
		t.Fatal("a group with zero children must be refused")
	}
	if _, err := ValidatePredicate(&Predicate{}, vpFields()); err == nil {
		t.Fatal("an empty node must be refused")
	}
}

func TestValidatePredicateRefusesGroupWithField(t *testing.T) {
	p := &Predicate{Field: "status", Children: []Predicate{{Field: "status", Op: OpEq, Value: "x"}}}
	if _, err := ValidatePredicate(p, vpFields()); err == nil {
		t.Fatal("a node that is both leaf and group must be refused")
	}
}
func TestValidatePredicateDepthBounded(t *testing.T) {
	// maxPredicateDepth wrappers over a leaf puts the leaf one past the cap.
	p := &Predicate{Field: "status", Op: OpEq, Value: "x"}
	for range maxPredicateDepth {
		p = &Predicate{Children: []Predicate{*p}}
	}
	if _, err := ValidatePredicate(p, vpFields()); err == nil {
		t.Fatal("over-depth tree must be refused")
	}
}

func TestValidatePredicateNodeCountBounded(t *testing.T) {
	kids := make([]Predicate, maxPredicateNodes+1)
	for i := range kids {
		kids[i] = Predicate{Field: "status", Op: OpEq, Value: "x"}
	}
	if _, err := ValidatePredicate(&Predicate{Children: kids}, vpFields()); err == nil {
		t.Fatal("over-cap node count must be refused")
	}
}

// mustParseWith is mustParse for an explicit field set.
func mustParseWith(t *testing.T, fields []schema.Field, raw string) *Predicate {
	t.Helper()
	p, err := ParseWhere(raw, fields)
	if err != nil {
		t.Fatalf("ParseWhere(%s): %v", raw, err)
	}
	return p
}
