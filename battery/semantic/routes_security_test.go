package semantic

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
		t.Fatalf("SECURITY: [semantic] POST /index leaked internal error text: %q", rec.Body.String())
	}
}

func TestHTTPQuery_DoesNotLeakInternalErrors(t *testing.T) {
	h := Handler(errIndex{queryErr: errors.New("dial tcp 10.0.0.5:11434: connect: refused")})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":"alpha","k":1}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "connect: refused") {
		t.Fatalf("SECURITY: [semantic] POST /query leaked internal error text: %q", body)
	}
}

func TestHTTPDelete_DoesNotLeakInternalErrors(t *testing.T) {
	h := Handler(errIndex{removeErr: errors.New("delete failed: internal bucket secret-bucket")})
	req := httptest.NewRequest(http.MethodDelete, "/doc/a", nil)
	req.SetPathValue("id", "a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "secret-bucket") {
		t.Fatalf("SECURITY: [semantic] DELETE /doc leaked internal error text: %q", rec.Body.String())
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
		t.Fatalf("SECURITY: [semantic] unauthenticated GET /stats returned %d. Attack: corpus statistics exposed without auth.", rec.Code)
	}
}

func TestHTTPDelete_RequiresAuthentication(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodDelete, "/doc/a", nil)
	req.SetPathValue("id", "a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [semantic] unauthenticated DELETE /doc returned %d. Attack: arbitrary document deletion without auth.", rec.Code)
	}
}

func TestHTTPIndex_RejectsTextPlainJSON(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[{"id":"a","text":"alpha"}]}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [semantic] POST /index accepted JSON body with text/plain content type (%d). Attack: content-type smuggling.", rec.Code)
	}
}

func TestHTTPIndex_RejectsMissingContentType(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[{"id":"a","text":"alpha"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [semantic] POST /index accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance.", rec.Code)
	}
}

func TestHTTPQuery_RejectsTextPlainJSON(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":"alpha","k":1}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [semantic] POST /query accepted JSON body with text/plain content type (%d). Attack: content-type smuggling.", rec.Code)
	}
}

func TestHTTPQuery_RejectsMissingContentType(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":"alpha","k":1}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [semantic] POST /query accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance.", rec.Code)
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
		t.Fatalf("SECURITY: [semantic] POST /index accepted oversized body (%d). Attack: unbounded JSON body DoS.", rec.Code)
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
		t.Fatalf("SECURITY: [semantic] POST /query accepted oversized body (%d). Attack: unbounded query body DoS.", rec.Code)
	}
}

func TestHTTPIndex_InvalidJSONDoesNotEchoDecoderInternals(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(`{"documents":[`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(strings.ToLower(rec.Body.String()), "unexpected eof") {
		t.Fatalf("SECURITY: [semantic] POST /index echoed decoder internals: %q", rec.Body.String())
	}
}

func TestHTTPQuery_InvalidJSONDoesNotEchoDecoderInternals(t *testing.T) {
	h := Handler(errIndex{})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(`{"text":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(strings.ToLower(rec.Body.String()), "unexpected eof") {
		t.Fatalf("SECURITY: [semantic] POST /query echoed decoder internals: %q", rec.Body.String())
	}
}

// Property: the /index and /query APIs refuse duplicate and case-folded
// top-level JSON keys. Stdlib encoding/json keeps the LAST duplicate and
// binds case-folded spellings onto the tagged field, so
// {"documents":[A],"documents":[B]} indexes batch B while an audit log
// (or intercepting proxy) records A, and a smuggled second "text"
// silently swaps the query the audit trail recorded — the ambiguity
// itself is the bug; strict decoding refuses it.
func TestRejectsDuplicateAndFoldedJSONKeys(t *testing.T) {
	cases := []struct {
		name  string // also the SECURITY message noun for where the keys collided
		scope string // index or query, for the message tag
		path  string
		body  string
		keys  string // what the request smuggled, for the failure message
		why   string // the consequence last-wins decoding would allow
	}{
		{
			name:  "index duplicate documents key",
			scope: "index",
			path:  "/index",
			// First occurrence indexes doc "a"; the smuggled last value
			// swaps the batch for doc "b".
			body: `{"documents":[{"id":"a","text":"alpha"}],"documents":[{"id":"b","text":"beta"}]}`,
			keys: `duplicate "documents" key`,
			why:  "last-wins swapped the indexed batch",
		},
		{
			name:  "index folded Documents key",
			scope: "index",
			path:  "/index",
			// "Documents" is not the wire tag, but stdlib json still binds
			// it via the case-insensitive tag fallback, so the folded
			// spelling smuggles a second batch exactly like the exact
			// duplicate does.
			body: `{"Documents":[{"id":"a","text":"alpha"}],"documents":[{"id":"b","text":"beta"}]}`,
			keys: `case-folded "Documents"/"documents" keys`,
			why:  "the folded spelling still binds the tagged field last-wins",
		},
		{
			name:  "query duplicate text key",
			scope: "query",
			path:  "/query",
			body:  `{"text":"alpha","text":"omega"}`,
			keys:  `duplicate "text" key`,
			why:   "last-wins swapped the query text",
		},
		{
			name:  "query folded Text key",
			scope: "query",
			path:  "/query",
			// "Text" binds the json:"text" tag via the case-insensitive
			// fallback, so the folded spelling smuggles a second query
			// value.
			body: `{"Text":"alpha","text":"omega"}`,
			keys: `case-folded "Text"/"text" keys`,
			why:  "the folded spelling still binds the tagged field last-wins",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler(errIndex{}, WithAuthToken("red-token"))
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer red-token")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("SECURITY: [semantic-%s-strict-keys] POST %s with %s returned %d %q — %s; want 400",
					tc.scope, tc.path, tc.keys, rec.Code, rec.Body.String(), tc.why)
			}
		})
	}
}

type removeRecorder struct {
	errIndex
	removed []string
}

func (r *removeRecorder) Remove(ctx context.Context, docIDs ...string) error {
	r.removed = append(r.removed, docIDs...)
	return nil
}

// DELETE /doc/a%FFb handed the index a 0xff byte, which a Postgres-backed
// index refuses in a text bind (22021): a 500. An id that is not storable
// text names no document, so the delete is the no-op an absent id gets,
// answered before the index sees it.
func TestHTTPDelete_UnstorableIDNeverReachesIndex(t *testing.T) {
	idx := &removeRecorder{}
	h := Handler(idx, WithAuthToken("test"))
	for _, target := range []string{"/doc/a%FFb", "/doc?id=a%00b"} {
		req := httptest.NewRequest(http.MethodDelete, target, nil)
		req.Header.Set("Authorization", "Bearer test")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("DELETE %s = %d %s, want 204", target, rec.Code, rec.Body.String())
		}
	}
	if len(idx.removed) != 0 {
		t.Errorf("the index was asked to remove %q; an id that is not storable text must not reach it", idx.removed)
	}
}

func TestHandler_EmptyBearerWithNoTokenRefused(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	Handler(errIndex{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an empty bearer matched the unset token: %d, want 401", rec.Code)
	}
}

func TestHTTPIndex_RejectsOversizedChunkedBody(t *testing.T) {
	huge := `{"documents":[{"id":"a","text":"` + strings.Repeat("A", 2<<20) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/index", bytes.NewBufferString(huge))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	Handler(errIndex{}, WithInsecureDisabledAuth()).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized body of unknown length = %d, want 413", rec.Code)
	}
}

func TestHandlerAnswersAreNoStore(t *testing.T) {
	idx, _ := Open(Options{Embedder: NewStubEmbedder(16)})
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	Handler(idx, WithInsecureDisabledAuth()).ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}
