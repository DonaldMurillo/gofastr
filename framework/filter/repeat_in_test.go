package filter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestRepeatedInKeysUnion pins the repeated-query-param contract: every
// occurrence of ?field_in= contributes its values. Standard HTTP clients
// (Rails/axios/jQuery arrays) naturally emit ?tag_in=a&tag_in=b for a
// multi-select; reading only values[0] silently narrowed the filter to
// "a" — the client asked for a union and got a subset without any error.
// Union semantics equal the comma form: ?tag_in=a&tag_in=b,c ≡ ?tag_in=a,b,c.
func TestRepeatedInKeysUnion(t *testing.T) {
	fields := []schema.Field{{Name: "tag", Type: schema.String}}

	req := httptest.NewRequest(http.MethodGet, "/?tag_in=a&tag_in=b,c", nil)
	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := map[string]bool{}
	for _, f := range filters {
		if f.Op == OpIn {
			got[f.Value] = true
		}
	}
	for _, want := range []string{"a", "b", "c"} {
		if !got[want] {
			t.Errorf("?tag_in=a&tag_in=b,c lost value %q — repeated keys must union, parsed: %v", want, filters)
		}
	}
}

// TestRepeatedInKeysOverCapErrors: the 1000-entry cap counts the UNION of
// all occurrences, so a caller can't bypass it by splitting one huge list
// across repeated keys (1101 entries as 2 keys must fail like 1 key does).
func TestRepeatedInKeysOverCapErrors(t *testing.T) {
	fields := []schema.Field{{Name: "tag", Type: schema.String}}

	var b1, b2 strings.Builder
	for i := range MaxINListEntries {
		if i > 0 {
			b1.WriteString(",")
		}
		b1.WriteString("v")
	}
	b2.WriteString("extra1,extra2") // +2 over the cap across both keys

	req := httptest.NewRequest(http.MethodGet, "/?tag_in="+b1.String()+"&tag_in="+b2.String(), nil)
	_, err := ParseFilters(req, fields)
	if err == nil {
		t.Fatal("repeated ?tag_in= keys totalling over the cap must error, not silently narrow")
	}
	if !strings.Contains(err.Error(), "max") || !strings.Contains(err.Error(), fmt.Sprint(MaxINListEntries)) {
		t.Errorf("error does not report the cap: %v", err)
	}
}
