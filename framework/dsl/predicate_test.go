package dsl

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func predTestFields() []schema.Field {
	return []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "kind", Type: schema.String},
		{Name: "vat_number", Type: schema.String},
		{Name: "amount", Type: schema.Int},
		{Name: "active", Type: schema.Bool},
		{Name: "due_on", Type: schema.Date},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "card", Type: schema.String, NoQuery: true},
	}
}

func TestParsePredicateEmptyIsNil(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		p, err := ParsePredicate(in, predTestFields())
		if err != nil || p != nil {
			t.Fatalf("ParsePredicate(%q) = (%v, %v), want (nil, nil)", in, p, err)
		}
	}
}

func TestParsePredicateSimpleEqual(t *testing.T) {
	p, err := ParsePredicate(`status = "open"`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if p.Field != "status" || p.Op != filter.OpEq || p.Value != "open" {
		t.Fatalf("predicate = %+v", p)
	}
}

func TestParsePredicateAllSymbolOps(t *testing.T) {
	cases := []struct {
		text string
		op   filter.FilterOp
	}{
		{`status != "open"`, filter.OpNe},
		{`amount < 10`, filter.OpLt},
		{`amount > 10`, filter.OpGt},
		{`amount <= 10`, filter.OpLte},
		{`amount >= 10000`, filter.OpGte},
	}
	for _, c := range cases {
		p, err := ParsePredicate(c.text, predTestFields())
		if err != nil {
			t.Fatalf("%s: %v", c.text, err)
		}
		if p.Op != c.op {
			t.Fatalf("%s: op = %v, want %v", c.text, p.Op, c.op)
		}
	}
}

func TestParsePredicateContainsIsOpLike(t *testing.T) {
	p, err := ParsePredicate(`status contains "open"`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if p.Op != filter.OpLike || p.Value != "open" {
		t.Fatalf("predicate = %+v", p)
	}
}

// contains must match the user's text literally: BuildPredicate escapes
// the LIKE metacharacters in the bound arg and appends ESCAPE, so a 50%
// in the filter cannot act as a wildcard. The end-to-end row match is
// pinned in framework/crud's ListAll tests against SQLite.
func TestParsePredicateContainsEscapesWildcards(t *testing.T) {
	p, err := ParsePredicate(`vat_number contains "50%"`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	c := filter.BuildPredicate(p)
	arg, _ := c.Args[0].(string)
	if arg != `%50\%%` {
		t.Fatalf("bound pattern = %q, want %%50\\%%", arg)
	}
	if !strings.Contains(c.SQL, `ESCAPE '\'`) {
		t.Fatalf("LIKE clause missing ESCAPE: %q", c.SQL)
	}
}

func TestParsePredicateInList(t *testing.T) {
	p, err := ParsePredicate(`status in ["open", "past_due"]`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if p.Op != filter.OpIn || len(p.Values) != 2 || p.Values[0] != "open" || p.Values[1] != "past_due" {
		t.Fatalf("predicate = %+v", p)
	}
}

func TestParsePredicatePlanExamples(t *testing.T) {
	for _, text := range []string{
		`status = "open"`,
		`amount >= 10000`,
		`status in ["open", "past_due"] and due_on < "2026-10-01"`,
		`(kind = "company" or vat_number != "") and active = true`,
	} {
		if _, err := ParsePredicate(text, predTestFields()); err != nil {
			t.Fatalf("ParsePredicate(%q): %v", text, err)
		}
	}
}

// A chain of ands (or ors) is one flat group, so a long plain filter is
// never refused by the depth cap that guards real nesting.
func TestParsePredicateLongChainIsFlat(t *testing.T) {
	terms := make([]string, 12)
	for i := range terms {
		terms[i] = `amount > 1`
	}
	for _, kw := range []string{" and ", " or "} {
		p, err := ParsePredicate(strings.Join(terms, kw), predTestFields())
		if err != nil {
			t.Fatalf("12-term%schain refused: %v", kw, err)
		}
		if len(p.Children) != 12 {
			t.Fatalf("12-term%schain has %d children, want one flat group", kw, len(p.Children))
		}
	}
}

func TestParsePredicateAndOrPrecedence(t *testing.T) {
	// a or b and c parses as or(a, and(b, c)): and binds tighter.
	p, err := ParsePredicate(`status = "a" or kind = "b" and vat_number = "c"`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if !p.Or || len(p.Children) != 2 {
		t.Fatalf("root = %+v", p)
	}
	right := p.Children[1]
	if right.Or || len(right.Children) != 2 {
		t.Fatalf("right child must be the AND group: %+v", right)
	}
}

func TestParsePredicateParensGroup(t *testing.T) {
	p, err := ParsePredicate(`(status = "a" or kind = "b") and vat_number = "c"`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if p.Or || len(p.Children) != 2 {
		t.Fatalf("root must be the AND group: %+v", p)
	}
	left := p.Children[0]
	if !left.Or || len(left.Children) != 2 {
		t.Fatalf("left child must be the OR group: %+v", left)
	}
}

func TestParsePredicateKeywordsCaseInsensitive(t *testing.T) {
	if _, err := ParsePredicate(`status = "a" AND kind = "b" OR active = TRUE`, predTestFields()); err != nil {
		t.Fatalf("case-insensitive keywords rejected: %v", err)
	}
	p, err := ParsePredicate(`active = TRUE`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if p.Value != "true" {
		t.Fatalf("bool spelling not normalized: %q", p.Value)
	}
}

func TestParsePredicateStringEscapes(t *testing.T) {
	p, err := ParsePredicate(`vat_number = "a\"b\\c"`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if p.Value != `a"b\c` {
		t.Fatalf("value = %q, want %q", p.Value, `a"b\c`)
	}
}

func TestParsePredicateOverSizeRefused(t *testing.T) {
	// Valid shape past the cap: were the size check removed, the text
	// would parse (a very long quoted value), so the refusal observable
	// here is the cap itself.
	big := `status = "` + strings.Repeat("a", maxDSLInputSize) + `"`
	if len(big) <= maxDSLInputSize {
		t.Fatal("test input must exceed the cap")
	}
	if _, err := ParsePredicate(big, predTestFields()); err == nil {
		t.Fatal("input over maxDSLInputSize must be refused")
	}
	if _, err := ParseSort(big, predTestFields()); err == nil {
		t.Fatal("ParseSort must refuse the same size bound")
	}
}
func TestParsePredicateFieldGuards(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"hidden", `secret = "x"`},
		{"noquery", `card = "4111"`},
		{"unknown", `nope = "x"`},
		{"sql metacharacters", `id; DROP TABLE x-- = "1"`},
		{"like on int", `amount contains "50"`},
	}
	for _, c := range cases {
		if _, err := ParsePredicate(c.text, predTestFields()); err == nil {
			t.Errorf("%s: ParsePredicate(%q) accepted", c.name, c.text)
		}
	}
}

func TestParsePredicateSyntaxErrors(t *testing.T) {
	cases := []string{
		`status =`,                               // missing value
		`status`,                                 // missing operator
		`= "x"`,                                  // missing field
		`status = bare_word`,                     // bare word must be quoted
		`status = "unterminated`,                 // unterminated string
		`status = "bad \n escape"`,               // invalid escape
		`status in "open"`,                       // in without a list
		`status in []`,                           // empty list
		`status in ["a" "b"]`,                    // missing comma
		`(status = "a"`,                          // unclosed paren
		`status = "a")`,                          // trailing paren
		`status = "a" and`,                       // dangling and
		`status = "a" or`,                        // dangling or
		`status = "a" status = "b"`,              // missing boolean connective
		`status = "a" amount >= 2 or kind = "b"`, // two ops
	}
	for _, text := range cases {
		if _, err := ParsePredicate(text, predTestFields()); err == nil {
			t.Errorf("ParsePredicate(%q) accepted", text)
		}
	}
}

// Errors name the offset and never echo control bytes or the whole payload.
func TestParsePredicateErrorsScrubbedAndPositioned(t *testing.T) {
	_, err := ParsePredicate("status = \"x\" and \x00 evil", predTestFields())
	if err == nil {
		t.Fatal("control byte input must fail somewhere")
	}
	if strings.ContainsRune(err.Error(), 0) {
		t.Fatalf("error echoes a NUL byte: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "offset") {
		t.Fatalf("error does not name a position: %v", err)
	}

	// A long payload is truncated to a short excerpt.
	long := "status = \"" + strings.Repeat("A", 4000) + "\" and ="
	_, err = ParsePredicate(long, predTestFields())
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > 200 {
		t.Fatalf("error is a payload dump (%d bytes)", len(err.Error()))
	}
}

func TestParsePredicateBoolCoercesAtBind(t *testing.T) {
	p, err := ParsePredicate(`active != false`, predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	c := filter.BuildPredicate(p)
	if b, ok := c.Args[0].(bool); !ok || b != false {
		t.Fatalf("arg = %#v, want Go false", c.Args[0])
	}
}

func TestParsePredicateAliasedFieldResolves(t *testing.T) {
	fields := []schema.Field{{Name: "due_on", Type: schema.Date, WireName: "dueDate"}}
	p, err := ParsePredicate(`dueDate = "2026-10-01"`, fields)
	if err != nil {
		t.Fatal(err)
	}
	if p.Field != "due_on" {
		t.Fatalf("wire name not resolved to column: %q", p.Field)
	}
}

// --- ParseSort ---

func TestParseSortBasic(t *testing.T) {
	sorts, err := ParseSort("due_on ASC", predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(sorts) != 1 || sorts[0].Field != "due_on" || sorts[0].Desc {
		t.Fatalf("sorts = %+v", sorts)
	}
}

func TestParseSortMultipleKeys(t *testing.T) {
	sorts, err := ParseSort("amount DESC, number ASC", []schema.Field{
		{Name: "amount", Type: schema.Int},
		{Name: "number", Type: schema.String},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sorts) != 2 || !sorts[0].Desc || sorts[1].Desc {
		t.Fatalf("sorts = %+v", sorts)
	}
}

func TestParseSortDirectionDefaultsAsc(t *testing.T) {
	sorts, err := ParseSort("status", predTestFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(sorts) != 1 || sorts[0].Desc {
		t.Fatalf("sorts = %+v", sorts)
	}
}

func TestParseSortCaseInsensitiveDirection(t *testing.T) {
	if _, err := ParseSort("status desc", predTestFields()); err != nil {
		t.Fatalf("lowercase direction refused: %v", err)
	}
}

func TestParseSortEmptyIsNil(t *testing.T) {
	sorts, err := ParseSort("  ", predTestFields())
	if err != nil || sorts != nil {
		t.Fatalf("ParseSort(blank) = (%v, %v)", sorts, err)
	}
}

func TestParseSortRefuses(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"unknown field", "nope ASC"},
		{"hidden field", "secret ASC"},
		{"noquery field", "card ASC"},
		{"bad direction", "status sideways"},
		{"not an identifier", "status;-- ASC"},
		{"empty key", "status ASC, "},
		{"too many keys", strings.Repeat("status ASC, ", maxSortTerms) + "kind ASC"},
	}
	for _, c := range cases {
		if _, err := ParseSort(c.text, predTestFields()); err == nil {
			t.Errorf("%s: ParseSort(%q) accepted", c.name, c.text)
		}
	}
}
