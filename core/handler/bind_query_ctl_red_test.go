//go:build red

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5;
// tests-only; no fix applied).
// Property: request-derived scalars bound into handler structs carry no raw
// control bytes that could not arrive through the transport — the identical
// property bindPath enforces via router.Param, pinned by
// TestBindPathParamsAreSanitized; the query twin is the unsanitized arrival.
// Surfaces: core/handler/bind.go bindQuery — setField(fv, values[0]) with the
// raw percent-decoded query value.
// Finding: GET /search?q=%0d%0aINJECTED%00%7f binds "\r\nINJECTED\x00\x7f"
// verbatim into a `query:"q"` field, from where it flows into logs, response
// headers, SSE frames, and file/DB lookups exactly like the path twin did.
// Fix direction: sanitize like bindPath (router.Param's sanitizePathParam
// rule) or refuse the value; either removes the raw bytes.

func TestBindQueryRedControlBytes(t *testing.T) {
	type searchInput struct {
		Q string `query:"q"`
	}

	// The transport delivers %0d%0a%00%7f; URL.Query() percent-decodes them
	// to raw CR LF NUL DEL before bindQuery ever runs.
	var in searchInput
	req := httptest.NewRequest(http.MethodGet, "/search?q=%0d%0aINJECTED%00%7f", nil)
	if err := Bind(req, &in); err != nil {
		t.Fatalf("Bind error: %v", err)
	}
	if got := in.Q; strings.ContainsAny(got, "\r\n\x00\x7f") {
		t.Errorf("SECURITY: [bind-query-ctl] query parameter bound raw control bytes into "+
			"handler input: q=%q. Attack: percent-encoded CR/LF/NUL/DEL reach handler structs "+
			"unsanitized (path params get the router.Param sanitize; the query twin must not "+
			"re-open the same smuggle into logs, headers, SSE, or file/DB lookups).", got)
	}

	// Positive control: a clean value round-trips unchanged.
	var clean searchInput
	req2 := httptest.NewRequest(http.MethodGet, "/search?q=hello+world", nil)
	if err := Bind(req2, &clean); err != nil {
		t.Fatalf("Bind error (clean control): %v", err)
	}
	if clean.Q != "hello world" {
		t.Errorf("SECURITY: [bind-query-ctl] positive control: clean query value did not "+
			"round-trip (got %q)", clean.Q)
	}
}
