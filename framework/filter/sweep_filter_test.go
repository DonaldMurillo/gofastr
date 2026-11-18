package filter

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

func sweepFields() []schema.Field {
	return []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String},
		{Name: "meta", Type: schema.JSON},
		{Name: "card_number", Type: schema.String, WireName: "pan", NoQuery: true},
		{Name: "check", Type: schema.String, Hidden: true},
		{Name: "check_in", Type: schema.String},
	}
}

func TestINListRendersOneClausePerKey(t *testing.T) {
	filters, err := parseRaw(t, "status_in=a,b,c", sweepFields())
	if err != nil {
		t.Fatal(err)
	}
	qb := query.Select("*").From("t")
	ApplyToQuery(qb, filters)
	sql, args := qb.Build()
	if strings.Count(sql, " IN (") != 1 || len(args) != 3 {
		t.Fatalf("one key's IN values must render as one IN clause: %s %v", sql, args)
	}
}

func TestNoQueryWireNameRefusedByName(t *testing.T) {
	_, err := parseRaw(t, "pan_like=4111", sweepFields())
	wantReason(t, "pan_like=4111", err, reasonNotFilterable)
	_, err = ParseSortValues(map[string][]string{"sort": {"-pan"}}, sweepFields())
	wantReason(t, "sort=-pan", err, reasonNotSortable)
}

func TestSortRefusesControlBytesByReason(t *testing.T) {
	_, err := ParseSortValues(map[string][]string{"sort": {"ti\x01tle"}}, sweepFields())
	wantReason(t, "sort=ti%01tle", err, reasonSortControlBytes)
}

func TestSuggestionNeedsOneCloseMatch(t *testing.T) {
	if got := nearestField("cax", []string{"cat", "car"}); got != "" {
		t.Errorf("a tie must suggest nothing, got %q", got)
	}
	if got := nearestField("zzzzzz", []string{"status"}); got != "" {
		t.Errorf("a far key must suggest nothing, got %q", got)
	}
	if got := nearestField("stauts", []string{"status", "title"}); got != "status" {
		t.Errorf("a close key must suggest status, got %q", got)
	}
}

func TestPlainEqualityOnJSONRefused(t *testing.T) {
	_, err := parseRaw(t, "meta=x", sweepFields())
	wantReason(t, "meta=x", err, reasonOpTypeMismatch)
}

func TestCoerceValueRefusesNonJSON(t *testing.T) {
	if _, err := CoerceValue("meta", schema.JSON, "{not json"); err == nil {
		t.Fatal("non-JSON text must not coerce for a JSON column")
	}
	if _, err := CoerceValue("meta", schema.JSON, `{"a":1}`); err != nil {
		t.Fatalf("JSON text must coerce: %v", err)
	}
}

func TestSpellerForSkipsHidden(t *testing.T) {
	if got := SpellerFor(sweepFields()).AmbiguousKeys(); len(got) != 0 {
		t.Fatalf("a Hidden field must make no key ambiguous, got %v", got)
	}
}

func TestWhereScalarOpRefusesValues(t *testing.T) {
	_, err := ParseWhere(`{"field":"title","op":"eq","value":"a","values":["b"]}`, sweepFields())
	wantReason(t, "where", err, reasonWhereStrayMember)
}

func TestValidatePredicateRefusesOverCapIn(t *testing.T) {
	p := &Predicate{Field: "title", Op: OpIn, Values: make([]string, MaxINListEntries+1)}
	_, err := ValidatePredicate(p, sweepFields())
	wantReason(t, "where", err, reasonWhereInOverCap)
}

func TestValidatePredicateRefusesUnknownOp(t *testing.T) {
	_, err := ValidatePredicate(&Predicate{Field: "title", Op: "zz", Value: "a"}, sweepFields())
	wantReason(t, "where", err, reasonWhereUnknownOp)
}

func TestSearchConditionsUseLikeClause(t *testing.T) {
	conds, err := SearchConditions([]string{"title", "status"}, "Ab%")
	if err != nil || len(conds) != 1 {
		t.Fatalf("conds %v, err %v", conds, err)
	}
	a, argA := LikeClause("title", 1, "Ab%")
	b, argB := LikeClause("status", 2, "Ab%")
	if conds[0].SQL != "("+a+" OR "+b+")" || conds[0].Args[0] != argA || conds[0].Args[1] != argB {
		t.Fatalf("search must compile through LikeClause: %s %v", conds[0].SQL, conds[0].Args)
	}
}
