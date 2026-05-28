package crud

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestList_RejectsHiddenSort guards against sort-by-hidden as an
// information-disclosure vector. A Hidden field never returns in the
// response body, but if the caller can sort by it, the row ordering
// leaks the value (e.g. "sort by api_key, page through results").
func TestList_RejectsHiddenSort(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "api_key", Type: schema.String, Hidden: true},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, api_key TEXT)`)
	seedRows(t, db, "items", []map[string]any{
		{"id": "i-1", "title": "a", "api_key": "AAA"},
		{"id": "i-2", "title": "b", "api_key": "BBB"},
	})

	rr := httptest.NewRecorder()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?sort=api_key"})
	ch.List()(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("List accepted sort on Hidden field; row order leaks the value (body=%s)", rr.Body.String())
	}
}

// TestList_RejectsUnknownSort pins fail-closed behavior on probe sorts.
// Silently dropping the unknown field made the API look like it worked
// the same with or without the param — an oracle for probe attempts and
// a footgun for legit clients with typos.
func TestList_RejectsUnknownSort(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT)`)
	seedRows(t, db, "items", []map[string]any{{"id": "i-1", "title": "a"}})

	rr := httptest.NewRecorder()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?sort=password"})
	ch.List()(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("List accepted unknown sort field (body=%s)", rr.Body.String())
	}
}

// TestList_RejectsControlBytesInSort: CR/LF/NUL/ESC in a sort value have
// no business reaching a SQL identifier path; reject upfront rather than
// trusting downstream escaping.
func TestList_RejectsControlBytesInSort(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT)`)
	seedRows(t, db, "items", []map[string]any{{"id": "i-1", "title": "a"}})

	for _, payload := range []string{"title\nadmin", "title\rprobe", "title\x00x", "title\x1b[0;31m"} {
		rr := httptest.NewRecorder()
		q := url.Values{"sort": []string{payload}}.Encode()
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?" + q})
		ch.List()(rr, req)
		if rr.Code == http.StatusOK {
			t.Fatalf("List accepted control-byte sort %q (body=%s)", payload, rr.Body.String())
		}
	}
}
