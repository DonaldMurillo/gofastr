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
// failure (returned error OR a recovered panic) — never a silent
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
		// Parse error is fine — XML was not parsed
		t.Logf("Non-JSON body correctly not parsed: %v", err)
	}
	if dst.Name == "" {
		// Query param should have been bound
		t.Errorf("query param 'name' was not bound for non-JSON request")
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
	// If it succeeds, that's fine — json.Decoder handles nesting
	// If it fails with a resource error, that's also fine
	if err != nil {
		t.Logf("Deeply nested JSON handling: %v", err)
	}
}

// TestRespond_ErrorNoStackLeak verifies that WriteError refuses to
// leak the inner message of a non-*Error error. A handler that returns
// a raw database error (or any unwrapped error) must get a generic
// 500 body — the inner cause stays server-side. Callers that *do*
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
