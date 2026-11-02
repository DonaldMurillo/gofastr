package filter

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// bracketFields is the colliding shape the bracket form exists for:
// check beside check_in (an _in suffix), price beside price_gte (a range
// suffix), plus a lone opt_in (no opt field) and a wire alias.
func bracketFields() []schema.Field {
	return []schema.Field{
		{Name: "check", Type: schema.String},
		{Name: "check_in", Type: schema.String},
		{Name: "price", Type: schema.Int},
		{Name: "price_gte", Type: schema.Int},
		{Name: "opt_in", Type: schema.Bool},
		{Name: "title", Type: schema.String},
		{Name: "author_id", Type: schema.Int, WireName: "writer"},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "ssn", Type: schema.String, NoQuery: true},
	}
}

func parseRaw(t *testing.T, raw string, fields []schema.Field, opts ...FilterOption) ([]ParsedFilter, error) {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", raw, err)
	}
	return ParseFiltersValues(q, fields, opts...)
}

func wantReason(t *testing.T, raw string, err error, reason i18n.Phrase) {
	t.Helper()
	var ie *i18n.Error
	if err == nil || !errors.As(err, &ie) || ie.Reason != reason {
		t.Fatalf("?%s: error %v, want %s", raw, err, reason.Key())
	}
}

// Every operator the suffix form takes is reachable by its word in
// brackets, and the name before the brackets is the whole field name:
// check_in[eq] is check_in = …, never check IN (…).
func TestBracketFormEveryOperator(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		field string
		op    FilterOp
		value string
	}{
		{"check_in[eq]=2026-11-01", "check_in", OpEq, "2026-11-01"},
		{"check[eq]=a", "check", OpEq, "a"},
		{"title[ne]=x", "title", OpNe, "x"},
		{"price[gt]=5", "price", OpGt, "5"},
		{"price[gte]=5", "price", OpGte, "5"},
		{"price_gte[gte]=5", "price_gte", OpGte, "5"},
		{"price[lt]=5", "price", OpLt, "5"},
		{"price[lte]=5", "price", OpLte, "5"},
		{"title[like]=foo", "title", OpLike, "foo"},
		{"check[in]=a", "check", OpIn, "a"},
		{"opt_in[eq]=true", "opt_in", OpEq, "true"},
		{"writer[gte]=3", "author_id", OpGte, "3"},
	} {
		fs, err := parseRaw(t, tc.raw, bracketFields())
		if err != nil {
			t.Fatalf("?%s: %v", tc.raw, err)
		}
		if len(fs) != 1 || fs[0].Field != tc.field || fs[0].Op != tc.op || fs[0].Value != tc.value {
			t.Fatalf("?%s: parsed %+v, want %s %s %q", tc.raw, fs, tc.field, tc.op, tc.value)
		}
	}
	// Every word in BracketOps is accepted, and each is the suffix's word.
	for _, op := range BracketOps {
		if op != OpEq && SuffixKey("x", op) != "x_"+string(op) {
			t.Errorf("bracket word %q is not its suffix's word (%q)", op, SuffixKey("x", op))
		}
	}
}

// ?check[in]= reads every occurrence, comma-split, like _in; and the
// IN list is the column's own, rendered as one IN clause.
func TestBracketInReadsEveryOccurrence(t *testing.T) {
	fs, err := parseRaw(t, "check[in]=a,b&check[in]=c", bracketFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 3 {
		t.Fatalf("check[in]=a,b&check[in]=c: %+v, want 3 values", fs)
	}
	qb := query.Select("*").From("t")
	ApplyToQuery(qb, fs)
	sql, args := qb.Build()
	if !strings.Contains(sql, "check IN ($1,$2,$3)") || len(args) != 3 {
		t.Fatalf("SQL %q args %v, want one 3-value IN", sql, args)
	}
}

// ?check_in=a&check[in]=b names one column under two keys: two IN lists
// ANDed, the rule a column and its wire alias already follow.
func TestSuffixAndBracketInAreTwoKeys(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}
	fs, err := parseRaw(t, "status_in=a,b&status[in]=c", fields)
	if err != nil {
		t.Fatal(err)
	}
	qb := query.Select("*").From("t")
	ApplyToQuery(qb, fs)
	sql, _ := qb.Build()
	if strings.Count(sql, " IN (") != 2 {
		t.Fatalf("SQL %q: want two IN lists ANDed, not one union", sql)
	}
	if fs[0].SameINList(fs[len(fs)-1]) {
		t.Fatal("status_in and status[in] values report one IN list")
	}
}

// A bare key that is a field and also another field plus a suffix reads
// two ways. It is refused, Lenient included, naming both readings and
// the bracket spellings that pick one.
func TestAmbiguousBareKeyRefused(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		names []string
	}{
		{"check_in=x", []string{`"check_in"`, `"check"`, `"check_in[eq]"`, `"check[in]"`}},
		{"price_gte=5", []string{`"price_gte"`, `"price"`, `"price_gte[eq]"`, `"price[gte]"`}},
	} {
		for _, opts := range [][]FilterOption{nil, {Lenient()}} {
			fs, err := parseRaw(t, tc.raw, bracketFields(), opts...)
			if len(fs) != 0 {
				t.Fatalf("?%s: built %+v from an ambiguous key", tc.raw, fs)
			}
			wantReason(t, tc.raw, err, reasonAmbiguousFilter)
			for _, n := range tc.names {
				if !strings.Contains(err.Error(), n) {
					t.Errorf("?%s: error %q does not name %s", tc.raw, err, n)
				}
			}
		}
	}
	// A wire alias collides the same way: field writer_in beside the
	// alias writer.
	fields := append(bracketFields(), schema.Field{Name: "writer_in", Type: schema.String})
	_, err := parseRaw(t, "writer_in=x", fields)
	wantReason(t, "writer_in=x", err, reasonAmbiguousFilter)
	// A NoQuery field still names a reading: ssn_in beside NoQuery ssn
	// would otherwise answer "ssn cannot be filtered" to a caller who
	// meant the ssn_in column.
	fields = append(bracketFields(), schema.Field{Name: "ssn_in", Type: schema.String})
	_, err = parseRaw(t, "ssn_in=x", fields)
	wantReason(t, "ssn_in=x", err, reasonAmbiguousFilter)
	if fs, err := parseRaw(t, "ssn_in[eq]=x", fields); err != nil || len(fs) != 1 || fs[0].Field != "ssn_in" {
		t.Fatalf("ssn_in[eq]=x: %+v, %v", fs, err)
	}
}

// A Hidden field names no reading: secret_in beside Hidden secret is the
// lone field secret_in, exactly as if secret did not exist.
func TestHiddenFieldMakesNoAmbiguity(t *testing.T) {
	fields := append(bracketFields(), schema.Field{Name: "secret_in", Type: schema.String})
	fs, err := parseRaw(t, "secret_in=x", fields)
	if err != nil || len(fs) != 1 || fs[0].Field != "secret_in" || fs[0].Op != OpEq {
		t.Fatalf("secret_in=x beside Hidden secret: %+v, %v; want secret_in = x", fs, err)
	}
}

// A lone opt_in (no opt field) and every other unambiguous key parse
// exactly as before.
func TestUnambiguousKeysUnchanged(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		field string
		op    FilterOp
	}{
		{"opt_in=true", "opt_in", OpEq},
		{"check=a", "check", OpEq},
		{"title_like=a", "title", OpLike},
		{"price_lt=3", "price", OpLt},
		{"writer=3", "author_id", OpEq},
		{"writer_in=3", "author_id", OpIn},
	} {
		fs, err := parseRaw(t, tc.raw, bracketFields())
		if err != nil || len(fs) != 1 || fs[0].Field != tc.field || fs[0].Op != tc.op {
			t.Fatalf("?%s: %+v, %v; want %s %s", tc.raw, fs, err, tc.field, tc.op)
		}
	}
}

// %5B and %5D decode to the same key the literal brackets make.
func TestPercentEncodedBrackets(t *testing.T) {
	for _, raw := range []string{"check_in%5Beq%5D=x", "check_in%5beq%5d=x", "check_in[eq%5D=x"} {
		fs, err := parseRaw(t, raw, bracketFields())
		if err != nil || len(fs) != 1 || fs[0].Field != "check_in" || fs[0].Op != OpEq {
			t.Fatalf("?%s: %+v, %v; want check_in = x", raw, fs, err)
		}
	}
}

// An operator word outside BracketOps is a 400 naming it, Lenient
// included: the caller named a real field, and dropping the key would
// answer every row.
func TestUnknownBracketOpRefused(t *testing.T) {
	for _, raw := range []string{"title[foo]=x", "title[]=x", "title[EQ]=x", "title[eq][eq]=x", "title[_in]=x"} {
		for _, opts := range [][]FilterOption{nil, {Lenient()}} {
			_, err := parseRaw(t, raw, bracketFields(), opts...)
			wantReason(t, raw, err, reasonUnknownBracketOp)
		}
	}
}

// A bracket on a Hidden field answers exactly as one on an absent field:
// the unknown-filter 400, whatever the operator word, so the bracket
// form is no Hidden-column oracle. Lenient drops both alike.
func TestBracketHiddenFieldParity(t *testing.T) {
	for _, op := range []string{"eq", "like", "in", "gte", "foo"} {
		hidden := "secret[" + op + "]=x"
		absent := "nosuch[" + op + "]=x"
		_, hErr := parseRaw(t, hidden, bracketFields())
		_, aErr := parseRaw(t, absent, bracketFields())
		wantReason(t, hidden, hErr, reasonUnknownFilter)
		wantReason(t, absent, aErr, reasonUnknownFilter)
		if !sameErrorShape(hErr.Error(), aErr.Error(), "secret", "nosuch") {
			t.Fatalf("hidden vs absent differ:\n hidden: %v\n absent: %v", hErr, aErr)
		}
		for _, raw := range []string{hidden, absent} {
			if fs, err := parseRaw(t, raw, bracketFields(), Lenient()); err != nil || len(fs) != 0 {
				t.Fatalf("lenient ?%s: %+v, %v; want dropped", raw, fs, err)
			}
		}
	}
	// A NoQuery field is refused by name, as ?ssn_like= is.
	_, err := parseRaw(t, "ssn[like]=1", bracketFields())
	wantReason(t, "ssn[like]=1", err, reasonNotFilterable)
}

// A bracket key sent twice is the repeated-scalar 400; [in] reads every
// occurrence; f and f[eq] are two keys, ANDed.
func TestRepeatedBracketKey(t *testing.T) {
	_, err := parseRaw(t, "price[lt]=100&price[lt]=5", bracketFields())
	wantReason(t, "price[lt]=100&price[lt]=5", err, reasonRepeatedFilter)
	if !strings.Contains(err.Error(), `"price[lt]"`) {
		t.Fatalf("error %q does not name the bracket key", err)
	}
	fs, err := parseRaw(t, "title=a&title[eq]=b", bracketFields())
	if err != nil || len(fs) != 2 {
		t.Fatalf("title=a&title[eq]=b: %+v, %v; want two ANDed equalities", fs, err)
	}
}

// The operator still has to suit the column's type: price[like] is the
// 400 price_like is.
func TestBracketOpTypeChecked(t *testing.T) {
	_, err := parseRaw(t, "price[like]=1", bracketFields())
	wantReason(t, "price[like]=1", err, reasonOpTypeMismatch)
	_, err = parseRaw(t, "price[gte]=abc", bracketFields())
	wantReason(t, "price[gte]=abc", err, reasonValueType)
}

// The unknown-filter hint reads the name before the brackets.
func TestBracketTypoSuggests(t *testing.T) {
	_, err := parseRaw(t, "titel[eq]=x", bracketFields())
	wantReason(t, "titel[eq]=x", err, reasonUnknownFilterHint)
	if !strings.Contains(err.Error(), `"title"`) {
		t.Fatalf("error %q does not suggest title", err)
	}
}

// KeySpeller keeps the suffix spelling for every key that reads one way
// and gives the bracket spelling exactly where the bare key collides.
func TestKeySpeller(t *testing.T) {
	sp := NewKeySpeller("check", "check_in", "price", "price_gte", "opt_in", "title")
	for _, tc := range []struct {
		name string
		op   FilterOp
		want string
	}{
		{"check", OpIn, "check[in]"},
		{"check_in", OpEq, "check_in[eq]"},
		{"check", OpEq, "check"},
		{"check", OpNe, "check_ne"},
		{"check_in", OpIn, "check_in_in"},
		{"price", OpGte, "price[gte]"},
		{"price_gte", OpEq, "price_gte[eq]"},
		{"price", OpGt, "price_gt"},
		{"opt_in", OpEq, "opt_in"},
		{"title", OpLike, "title_like"},
	} {
		if got := sp.Key(tc.name, tc.op); got != tc.want {
			t.Errorf("Key(%s, %s) = %q, want %q", tc.name, tc.op, got, tc.want)
		}
		// Whatever it spells parses back to the same field and operator.
		named := sp.Named
		name, op, ok, err := ReadKey(sp.Key(tc.name, tc.op), named)
		if !ok || err != nil || name != tc.name || op != tc.op {
			t.Errorf("ReadKey(Key(%s, %s)) = %s %s %v %v", tc.name, tc.op, name, op, ok, err)
		}
	}
}
