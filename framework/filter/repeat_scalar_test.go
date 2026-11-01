package filter

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestRepeatedScalarFilterKeyRefused pins the refusal of a scalar filter
// key sent more than once. The parser kept values[0] and dropped the
// rest, so ?price_lt=100&price_lt=5 ran price < 100 (the dropped bound
// widened the result) while a proxy or WAF that reads the LAST value saw
// price < 5. Only _in reads every occurrence; any other repeated key is
// a 400 naming it, Lenient included.
func TestRepeatedScalarFilterKeyRefused(t *testing.T) {
	fields := []schema.Field{
		{Name: "price", Type: schema.Int},
		{Name: "status", Type: schema.String},
		{Name: "author_id", Type: schema.Int, WireName: "writer"},
	}
	for _, tc := range []struct{ raw, key string }{
		{"price_lt=100&price_lt=5", "price_lt"},
		{"status=a&status=b", "status"},
		{"writer=1&writer=2", "writer"},
		{"status_ne=a&status_ne=a", "status_ne"},
		{"status=&status=b", "status"},
	} {
		q, err := url.ParseQuery(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range [][]FilterOption{nil, {Lenient()}} {
			fs, err := ParseFiltersValues(q, fields, opts...)
			if err == nil {
				t.Fatalf("?%s (lenient=%v): accepted as %+v; a repeated scalar key must be refused", tc.raw, opts != nil, fs)
			}
			var ie *i18n.Error
			if !errors.As(err, &ie) || ie.Reason != reasonRepeatedFilter {
				t.Fatalf("?%s: error %v, want filter.repeated_filter", tc.raw, err)
			}
			if !strings.Contains(err.Error(), `"`+tc.key+`"`) {
				t.Fatalf("?%s: error %q does not name %q", tc.raw, err, tc.key)
			}
		}
	}
	// The same key once, and a repeated _in key, still parse.
	q, _ := url.ParseQuery("price_lt=100&price_gt=5&status_in=a&status_in=b")
	fs, err := ParseFiltersValues(q, fields)
	if err != nil || len(fs) != 4 {
		t.Fatalf("single scalar keys + repeated _in: %+v, %v; want 4 filters", fs, err)
	}
}
