package filter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestTopLevelINListOverCapErrors pins that a top-level ?field_in= list
// longer than MaxINListEntries is REJECTED with an error, not silently
// truncated. Silent truncation (parts[:MaxINListEntries]) narrows the
// predicate, every row past entry N drops out of the result set without
// the caller being told. The include-scoped sibling (parseScopedFilters)
// errors on its cap for the same reason; the top-level path must match.
func TestTopLevelINListOverCapErrors(t *testing.T) {
	fields := []schema.Field{{Name: "tag", Type: schema.String}}

	var b strings.Builder
	b.WriteString("/?tag_in=")
	for i := range MaxINListEntries + 1 { // MaxINListEntries+1 entries
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "v%d", i)
	}
	req := httptest.NewRequest(http.MethodGet, b.String(), nil)

	filters, err := ParseFilters(req, fields)
	if err == nil {
		t.Fatalf("ParseFilters silently truncated an IN list of %d entries to %d (got %d filters); want a 400-shaped error",
			MaxINListEntries+1, MaxINListEntries, len(filters))
	}

	// Message style mirrors parseScopedFilters' cap: names the field and
	// both counts so a generated client can surface the limit verbatim.
	msg := err.Error()
	if !strings.Contains(msg, "tag") {
		t.Errorf("error does not name the field: %q", msg)
	}
	if !strings.Contains(msg, fmt.Sprintf("%d", MaxINListEntries+1)) || !strings.Contains(msg, fmt.Sprintf("%d", MaxINListEntries)) {
		t.Errorf("error does not report actual+max counts: %q", msg)
	}
}

// TestTopLevelINListAtCapAccepted guards the boundary: a list exactly at
// the cap must still parse cleanly so the cap is a strict >, not >=.
func TestTopLevelINListAtCapAccepted(t *testing.T) {
	fields := []schema.Field{{Name: "tag", Type: schema.String}}

	var b strings.Builder
	b.WriteString("/?tag_in=")
	for i := range MaxINListEntries { // exactly MaxINListEntries
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "v%d", i)
	}
	req := httptest.NewRequest(http.MethodGet, b.String(), nil)

	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Fatalf("IN list at exactly the cap (%d) should parse, got: %v", MaxINListEntries, err)
	}
	if len(filters) != MaxINListEntries {
		t.Fatalf("expected %d filters, got %d", MaxINListEntries, len(filters))
	}
}
