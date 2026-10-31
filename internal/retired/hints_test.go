package retired

import (
	"fmt"
	"net/http"
	"testing"
)

// A 103 Early Hints fixed the body kind from the header as it stood at
// the hint, so a page whose real Content-Type came after went unscanned.
func TestMiddlewareScansPageAfterHints(t *testing.T) {
	_, got := serveMW(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Link", "</app.css>; rel=preload; as=style")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	if !equalStrings(got, wantPageFindings) {
		t.Fatalf("reports = %q, want %q", got, wantPageFindings)
	}
}
