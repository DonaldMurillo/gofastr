package embed

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errIndex struct {
	addErr    error
	queryErr  error
	removeErr error
}

func (e errIndex) Add(ctx context.Context, docs ...Document) error    { return e.addErr }
func (e errIndex) Remove(ctx context.Context, docIDs ...string) error { return e.removeErr }
func (e errIndex) Query(ctx context.Context, q Query) ([]Hit, error)  { return nil, e.queryErr }
func (e errIndex) Snapshot() error                                    { return nil }
func (e errIndex) Stats() Stats                                       { return Stats{} }
func (e errIndex) Close() error                                       { return nil }

func TestHTTPIndex_DoesNotLeakInternalErrors(t *testing.T) {
	h := Handler(errIndex{addErr: errors.New("upstream embed store password=secret")})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[{"id":"a","text":"alpha"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "password=secret") {
		t.Fatalf("SECURITY: [embed] POST /index leaked internal error text: %q", rec.Body.String())
	}
}

func TestHTTPQuery_DoesNotLeakInternalErrors(t *testing.T) {
	h := Handler(errIndex{queryErr: errors.New("dial tcp 10.0.0.5:11434: connect: refused")})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":"alpha","k":1}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "connect: refused") {
		t.Fatalf("SECURITY: [embed] POST /query leaked internal error text: %q", body)
	}
}

func TestHTTPDelete_DoesNotLeakInternalErrors(t *testing.T) {
	h := Handler(errIndex{removeErr: errors.New("delete failed: internal bucket secret-bucket")})
	req := httptest.NewRequest(http.MethodDelete, "/doc/a", nil)
	req.SetPathValue("id", "a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "secret-bucket") {
		t.Fatalf("SECURITY: [embed] DELETE /doc leaked internal error text: %q", rec.Body.String())
	}
}

// POST /index and /query auth: shape validation (Content-Type) runs
// before auth on JSON POST routes, so the 401 contract is tested on
// /stats and DELETE /doc, which have no body-shape gate.

func TestHTTPStats_RequiresAuthentication(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [embed] unauthenticated GET /stats returned %d. Attack: corpus statistics exposed without auth.", rec.Code)
	}
}

func TestHTTPDelete_RequiresAuthentication(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodDelete, "/doc/a", nil)
	req.SetPathValue("id", "a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [embed] unauthenticated DELETE /doc returned %d. Attack: arbitrary document deletion without auth.", rec.Code)
	}
}

func TestHTTPIndex_RejectsTextPlainJSON(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[{"id":"a","text":"alpha"}]}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [embed] POST /index accepted JSON body with text/plain content type (%d). Attack: content-type smuggling.", rec.Code)
	}
}

func TestHTTPIndex_RejectsMissingContentType(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[{"id":"a","text":"alpha"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [embed] POST /index accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance.", rec.Code)
	}
}

func TestHTTPQuery_RejectsTextPlainJSON(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":"alpha","k":1}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [embed] POST /query accepted JSON body with text/plain content type (%d). Attack: content-type smuggling.", rec.Code)
	}
}

func TestHTTPQuery_RejectsMissingContentType(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":"alpha","k":1}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [embed] POST /query accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance.", rec.Code)
	}
}

func TestHTTPIndex_RejectsOversizedBody(t *testing.T) {
	h := Handler(errIndex{})
	huge := `{"documents":[{"id":"a","text":"` + strings.Repeat("A", 2<<20) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(huge))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [embed] POST /index accepted oversized body (%d). Attack: unbounded JSON body DoS.", rec.Code)
	}
}

func TestHTTPQuery_RejectsOversizedBody(t *testing.T) {
	h := Handler(errIndex{})
	huge := `{"text":"` + strings.Repeat("Q", 2<<20) + `","k":1}`
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(huge))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [embed] POST /query accepted oversized body (%d). Attack: unbounded query body DoS.", rec.Code)
	}
}

func TestHTTPIndex_InvalidJSONDoesNotEchoDecoderInternals(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(strings.ToLower(rec.Body.String()), "unexpected eof") {
		t.Fatalf("SECURITY: [embed] POST /index echoed decoder internals: %q", rec.Body.String())
	}
}

func TestHTTPQuery_InvalidJSONDoesNotEchoDecoderInternals(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(strings.ToLower(rec.Body.String()), "unexpected eof") {
		t.Fatalf("SECURITY: [embed] POST /query echoed decoder internals: %q", rec.Body.String())
	}
}
