package filter

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func strictFields() []schema.Field {
	return []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "priority", Type: schema.String},
		{Name: "score", Type: schema.Int},
		{Name: "secret", Type: schema.String, Hidden: true},
	}
}

func parseQuery(t *testing.T, rawQuery string, opts ...FilterOption) ([]ParsedFilter, error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/things?"+rawQuery, nil)
	return ParseFilters(r, strictFields(), opts...)
}

// A misspelled top-level filter must NOT silently return unfiltered data —
// it must be rejected so a broken client can't read the whole table. This is
// the core #100A correctness contract.
func TestStrictRejectsUnknownFilter(t *testing.T) {
	_, err := parseQuery(t, "stauts=active")
	if err == nil {
		t.Fatal("unknown filter key must error, got nil (silent unfiltered result)")
	}
	if !strings.Contains(err.Error(), "stauts") {
		t.Fatalf("error must name the bad key, got %q", err)
	}
}

// A misspelled suffixed operator (?score_gt vs ?scor_gt) must also reject,
// not fall through and drop silently.
func TestStrictRejectsUnknownSuffixedFilter(t *testing.T) {
	if _, err := parseQuery(t, "scor_gt=5"); err == nil {
		t.Fatal("unknown suffixed filter base must error")
	}
}

// Filtering on a hidden column must fail closed (mirrors ParseSort) without
// leaking that the field exists-but-hidden vs does-not-exist.
func TestStrictRejectsHiddenFilter(t *testing.T) {
	if _, err := parseQuery(t, "secret=x"); err == nil {
		t.Fatal("hidden field filter must error")
	}
}

// Reserved list-control params are not entity fields and must never be
// rejected as unknown filters.
func TestStrictAllowsReservedControls(t *testing.T) {
	reserved := "sort=-score&page=2&limit=10&offset=0&cursor=abc&direction=next" +
		"&where=%7B%7D&fields=status&include=author&trashed=true&stream=true&q=hi"
	if _, err := parseQuery(t, reserved); err != nil {
		t.Fatalf("reserved controls must not be rejected, got %v", err)
	}
}

// Nested relation filters (?author.name=alice) are validated elsewhere
// (parseNestedFilters); ParseFilters must skip dotted keys, not reject them.
func TestStrictSkipsNestedDottedKeys(t *testing.T) {
	if _, err := parseQuery(t, "author.name=alice"); err != nil {
		t.Fatalf("dotted nested key must be skipped, got %v", err)
	}
}

// Known fields (plain and suffixed) still parse into filters.
func TestStrictAllowsKnownFields(t *testing.T) {
	filters, err := parseQuery(t, "status=open&score_gte=3")
	if err != nil {
		t.Fatalf("known fields must parse, got %v", err)
	}
	if len(filters) != 2 {
		t.Fatalf("want 2 filters, got %d: %v", len(filters), filters)
	}
}

// A close misspelling gets a "did you mean" suggestion when unambiguous.
func TestStrictSuggestsNearestField(t *testing.T) {
	_, err := parseQuery(t, "statuss=open")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Fatalf("error should suggest nearest field 'status', got %q", err)
	}
}

// Lenient() restores the pre-strict behavior: unknown keys are dropped and
// the known ones still parse. This is the documented escape hatch.
func TestLenientDropsUnknown(t *testing.T) {
	filters, err := parseQuery(t, "stauts=active&status=open", Lenient())
	if err != nil {
		t.Fatalf("lenient must not error, got %v", err)
	}
	if len(filters) != 1 || filters[0].Field != "status" {
		t.Fatalf("lenient should keep only the known filter, got %v", filters)
	}
}

// A declared field whose name collides with a reserved control word must be
// FILTERED, never swallowed by the reserved-skip — otherwise ?stream=true
// returns the whole table (the exact #100A hazard). Field wins over reserved.
func TestStrictFieldWinsOverReservedName(t *testing.T) {
	fields := []schema.Field{
		{Name: "stream", Type: schema.String},
		{Name: "q", Type: schema.String},
	}
	r := httptest.NewRequest("GET", "/x?stream=live&q=hi", nil)
	filters, err := ParseFilters(r, fields)
	if err != nil {
		t.Fatalf("field named like a reserved word must not error: %v", err)
	}
	got := map[string]string{}
	for _, f := range filters {
		got[f.Field] = f.Value
	}
	if got["stream"] != "live" || got["q"] != "hi" {
		t.Fatalf("reserved-named fields must be filtered, got %v", filters)
	}
}

// A field sent both plain and suffixed (?priority=high&priority_gte=2) must
// never be misreported as an unknown filter regardless of map-iteration
// order — the suffixed op is consumed and the plain key is a KNOWN field.
func TestStrictConsumedFieldNotUnknown(t *testing.T) {
	// Run many times: Go map iteration order is randomized, so a flaky
	// misclassification would surface across iterations.
	for i := 0; i < 50; i++ {
		filters, err := parseQuery(t, "priority=high&priority_gte=2")
		if err != nil {
			t.Fatalf("known field sent plain+suffixed must not error, got %v", err)
		}
		hasGte := false
		for _, f := range filters {
			if f.Field == "priority" && f.Op == OpGte {
				hasGte = true
			}
		}
		if !hasGte {
			t.Fatalf("expected priority_gte filter, got %v", filters)
		}
	}
}

// Allow() lets a host declare custom query params (consumed by a BeforeList
// hook or middleware) so strict parsing skips them instead of 400ing —
// without disabling strictness for genuine typos.
func TestStrictAllowsDeclaredExtraParams(t *testing.T) {
	filters, err := parseQuery(t, "region=eu&status=open", Allow("region"))
	if err != nil {
		t.Fatalf("declared extra param must not error, got %v", err)
	}
	if len(filters) != 1 || filters[0].Field != "status" {
		t.Fatalf("extra param must be skipped, known field kept, got %v", filters)
	}
	// A typo that is NOT declared still fails closed.
	if _, err := parseQuery(t, "regionn=eu", Allow("region")); err == nil {
		t.Fatal("undeclared unknown param must still error")
	}
}
