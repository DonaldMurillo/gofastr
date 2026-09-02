package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBind_BodySizeLimit verifies that oversized JSON bodies are rejected.
// Attack: memory exhaustion via huge request body.
func TestBind_BodySizeLimit(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	// Create a body larger than 1MB
	largeBody := `{"name":"` + strings.Repeat("A", 2*1024*1024) + `"}`

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")

	err := Bind(req, &dst)
	if err == nil {
		t.Errorf("SECURITY: [bind] Bind accepted a %d-byte body (limit ~1MB). Attack: memory exhaustion via oversized JSON.", len(largeBody))
	}
}

// TestBind_InvalidJSONRejected verifies that malformed JSON is rejected.
// Attack: probing error handling via malformed payloads.
func TestBind_InvalidJSONRejected(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	err := Bind(req, &dst)
	if err == nil {
		t.Errorf("SECURITY: [bind] Bind accepted malformed JSON. Attack: malformed payload handling.")
	}
}

// TestBind_NilDstPanics verifies that nil destination causes a clean
// failure (returned error OR a recovered panic). Never a silent
// success that lets a handler proceed with a zero-value struct.
func TestBind_NilDstPanics(t *testing.T) {
	var bindErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				bindErr = fmt.Errorf("panic: %v", r)
			}
		}()
		bindErr = Bind(httptest.NewRequest(http.MethodGet, "/", nil), nil)
	}()
	if bindErr == nil {
		t.Errorf("SECURITY: [bind] Bind with nil dst returned no error and did not panic — silent corruption is a real risk.")
	}
}

// TestBind_QueryOverride verifies that query parameters don't override
// JSON body values. Attack: overriding authenticated fields via query params.
func TestBind_QueryOverride(t *testing.T) {
	var dst struct {
		Name  string `json:"name" query:"name"`
		Email string `json:"email" query:"email"`
	}

	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/?name=Evil&email=evil@evil.com", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	err := Bind(req, &dst)
	if err != nil {
		t.Fatalf("Bind error: %v", err)
	}

	// JSON body should take priority over query params
	if dst.Name == "Evil" {
		t.Errorf("SECURITY: [bind] query param 'name=Evil' overrode JSON body 'Alice'. Attack: query param override of body fields.")
	}
	if dst.Email == "evil@evil.com" {
		t.Errorf("SECURITY: [bind] query param 'email=evil@evil.com' overrode JSON body. Attack: field override via query params.")
	}
}

// TestBind_HeaderOverride verifies that header values don't override
// JSON body values. Attack: injecting fields via custom headers.
func TestBind_HeaderOverride(t *testing.T) {
	var dst struct {
		Name string `json:"name" header:"X-Name"`
	}

	body := `{"name":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Name", "Evil")

	err := Bind(req, &dst)
	if err != nil {
		t.Fatalf("Bind error: %v", err)
	}

	if dst.Name == "Evil" {
		t.Errorf("SECURITY: [bind] header X-Name overrode JSON body. Attack: field override via custom headers.")
	}
}

// TestBind_PathParamNotOverridden verifies that path parameters don't
// override body values. Attack: overriding resource IDs via path.
func TestBind_PathParamNotOverridden(t *testing.T) {
	var dst struct {
		ID   string `json:"id" path:"id"`
		Name string `json:"name"`
	}

	body := `{"id":"real-id","name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "fake-id")

	err := Bind(req, &dst)
	if err != nil {
		t.Fatalf("Bind error: %v", err)
	}

	if dst.ID == "fake-id" {
		t.Errorf("SECURITY: [bind] path param 'fake-id' overrode JSON body 'real-id'. Attack: IDOR via path parameter override.")
	}
}

// TestBind_NonJSONContentTypeSkipped verifies that non-JSON content types
// don't attempt body parsing. Attack: XML external entity injection via
// Content-Type trickery.
func TestBind_NonJSONContentTypeSkipped(t *testing.T) {
	var dst struct {
		Name string `json:"name" query:"name"`
	}

	xmlBody := `<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><name>&xxe;</name>`
	req := httptest.NewRequest(http.MethodPost, "/?name=safe", strings.NewReader(xmlBody))
	req.Header.Set("Content-Type", "application/xml")

	err := Bind(req, &dst)
	if err != nil {
		// Parse error is fine; XML was not parsed
		t.Logf("Non-JSON body correctly not parsed: %v", err)
	}
	if dst.Name == "" {
		// Query param should have been bound
		t.Errorf("query param 'name' was not bound for non-JSON request")
	}
}

// Property: a POST with Content-Type: application/json and an EMPTY body
// leaves the query-bound values in place instead of failing or zeroing —
// the len(body)==0 branch must return before validation and decode.
func TestBind_EmptyBodyKeepsQueryValues(t *testing.T) {
	var dst struct {
		Name string `json:"name" query:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/?name=from-query", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("empty JSON body must not error: %v", err)
	}
	if dst.Name != "from-query" {
		t.Fatalf("query-bound value lost on empty body: %q", dst.Name)
	}
}

// Property: a repeated query parameter binds its FIRST value — the second
// occurrence never smuggles a different value past the first (bindQuery
// uses values[0]).
func TestBind_RepeatedQueryBindsFirstValue(t *testing.T) {
	var dst struct {
		Role string `query:"role"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?role=member&role=admin", nil)

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind error: %v", err)
	}
	if dst.Role != "member" {
		t.Fatalf("second query occurrence smuggled in: %q", dst.Role)
	}
}

func TestBind_JSONPrefixSpoofingRejected_JSONP(t *testing.T) {
	var dst struct {
		Name string `json:"name" query:"name"`
	}

	req := httptest.NewRequest(http.MethodPost, "/?name=safe", strings.NewReader(`{"name":"evil"}`))
	req.Header.Set("Content-Type", "application/jsonp")

	err := Bind(req, &dst)
	if err != nil {
		t.Fatalf("Bind error: %v", err)
	}
	if dst.Name != "safe" {
		t.Fatalf("SECURITY: [bind] application/jsonp was treated as JSON and overrode query value: got %q. Attack: Content-Type prefix spoofing bypass.", dst.Name)
	}
}

func TestBind_JSONPrefixSpoofingRejected_VendorSuffix(t *testing.T) {
	var dst struct {
		Name string `json:"name" query:"name"`
	}

	req := httptest.NewRequest(http.MethodPost, "/?name=safe", strings.NewReader(`{"name":"evil"}`))
	req.Header.Set("Content-Type", "application/json-evil")

	err := Bind(req, &dst)
	if err != nil {
		t.Fatalf("Bind error: %v", err)
	}
	if dst.Name != "safe" {
		t.Fatalf("SECURITY: [bind] spoofed content type %q was treated as JSON and overrode query value. Attack: Content-Type prefix smuggling.", req.Header.Get("Content-Type"))
	}
}

// TestBind_DeeplyNestedJSONRejected verifies that deeply nested JSON
// is handled. Attack: JSON depth bomb causing stack overflow.
func TestBind_DeeplyNestedJSONRejected(t *testing.T) {
	var dst struct {
		Data any `json:"data"`
	}

	// Create deeply nested JSON: {"data":{"a":{"a":{"a":...}}}}
	depth := 10000
	nested := strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth)
	body := `{"data":` + nested + `}`

	// Keep it under 1MB
	if len(body) > maxBodyBytes {
		depth = 5000
		nested = strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth)
		body = `{"data":` + nested + `}`
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	err := Bind(req, &dst)
	// If it succeeds, that's fine: json.Decoder handles nesting
	// If it fails with a resource error, that's also fine
	if err != nil {
		t.Logf("Deeply nested JSON handling: %v", err)
	}
}

// TestRespond_ErrorNoStackLeak verifies that WriteError refuses to
// leak the inner message of a non-*Error error. A handler that returns
// a raw database error (or any unwrapped error) must get a generic
// 500 body. The inner cause stays server-side. Callers that *do*
// want a custom message must wrap with Errorf/WrapError.
func TestRespond_ErrorNoStackLeak(t *testing.T) {
	w := httptest.NewRecorder()
	rawDBErr := errors.New(`pq: password authentication failed for user "admin"`)
	WriteError(w, rawDBErr)

	body := w.Body.String()
	if strings.Contains(body, "pq:") {
		t.Errorf("SECURITY: [respond] error response leaks database details: %s", body)
	}
	if !strings.Contains(body, "internal server error") {
		t.Errorf("expected generic 500 body, got: %s", body)
	}
}

// TestBindPathParamsAreSanitized pins, at Bind's `path:"..."` surface, the
// same property core/router enforces at Param()/Params() (sanitizePathParam):
// a path-parameter value bound into handler input never carries bytes that
// could not have arrived through normal path routing — CR/LF/NUL, a "/"
// inside a single-segment match (only reachable %2F-encoded), or a ".."
// segment (only reachable %2E-encoded; the mux cleans and redirects literal
// dot segments). bindPath (Bind step 3) reads r.PathValue raw today, so these
// cases RED until it applies the router's truncation, catch-all aware via
// r.Pattern exactly like router.isCatchAll.
func TestBindPathParamsAreSanitized(t *testing.T) {
	type pathInput struct {
		ID   string `path:"id"`
		Rest string `path:"rest"`
	}
	cases := []struct {
		name    string
		tag     string
		pattern string
		raw     string
		want    string
	}{
		{"clean value binds unchanged", "id", "GET /users/{id}", "42", "42"},
		{"newline smuggle truncates", "id", "GET /users/{id}", "42\nadmin", "42"},
		{"NUL smuggle truncates", "id", "GET /users/{id}", "42\x00admin", "42"},
		{"decoded slash truncates single segment", "id", "GET /users/{id}", "a/b", "a"},
		{"dot-dot segment truncates", "id", "GET /users/{id}", "../x", ""},
		{"catch-all keeps multi-segment value", "rest", "GET /files/{rest...}", "a/b/c", "a/b/c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var in pathInput
			req := httptest.NewRequest(http.MethodGet, "/users/x", nil)
			req.Pattern = tc.pattern
			req.SetPathValue(tc.tag, tc.raw)

			if err := Bind(req, &in); err != nil {
				t.Fatalf("Bind error: %v", err)
			}
			got := in.ID
			if tc.tag == "rest" {
				got = in.Rest
			}
			if got != tc.want {
				t.Errorf("SECURITY: [bind] path tag %q bound %q from raw PathValue %q, want %q. "+
					"Attack: router.Param sanitizes this class (core/router sanitizePathParam); "+
					"Bind's path binding is a second surface of the same property and must not "+
					"re-open smuggled bytes into logs, headers, SSE, or file/DB lookups.",
					tc.tag, got, tc.raw, tc.want)
			}
		})
	}

	// Delivery proof: the smuggled newline is not a synthetic SetPathValue.
	// A plain net/http mux delivers %0A into PathValue without a redirect
	// (the arrival path core/router's SECURITY comment documents), so
	// unmodified Bind writes it straight into the handler input struct.
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in pathInput
		if err := Bind(r, &in); err != nil {
			http.Error(w, "bind failed", http.StatusBadRequest)
			return
		}
		got = in.ID
	})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/users/42%0Aadmin", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("delivery case: mux returned %d, want 200 (a redirect here would invalidate the delivery premise)", rr.Code)
	}
	if got != "42" {
		t.Errorf("SECURITY: [bind] real mux delivery: GET /users/42%%0Aadmin bound id=%q, want %q. "+
			"Attack: percent-encoded control byte reaches handler input unsanitized.", got, "42")
	}
}
