package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*httptest.Server, Index) {
	t.Helper()
	idx, err := Open(Options{Embedder: NewStubEmbedder(64)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	srv := httptest.NewServer(Handler(idx))
	t.Cleanup(srv.Close)
	return srv, idx
}

// doAuthed performs an HTTP request with a dummy Authorization header.
// Handler() requires a non-empty Authorization on every route — see the
// security tests in routes_security_test.go for the contract.
func doAuthed(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	req.Header.Set("Authorization", "Bearer test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func postJSONAuthed(t *testing.T, url string, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return doAuthed(t, req)
}

func getAuthed(t *testing.T, url string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	return doAuthed(t, req)
}

func TestHTTPRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)

	// Index two docs via POST /index.
	resp := postJSONAuthed(t, srv.URL+"/index", `{"documents":[
		{"id":"a","text":"alpha bravo"},
		{"id":"b","text":"charlie delta"}
	]}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /index status = %d, want 202", resp.StatusCode)
	}
	var added indexResponse
	json.NewDecoder(resp.Body).Decode(&added)
	resp.Body.Close()
	if added.Added != 2 {
		t.Fatalf("added = %d, want 2", added.Added)
	}

	// GET /stats.
	resp = getAuthed(t, srv.URL+"/stats")
	var s Stats
	json.NewDecoder(resp.Body).Decode(&s)
	resp.Body.Close()
	if s.Docs != 2 {
		t.Fatalf("stats.Docs = %d, want 2", s.Docs)
	}

	// POST /query.
	resp = postJSONAuthed(t, srv.URL+"/query", `{"text":"alpha bravo","k":1}`)
	var qr queryResponse
	json.NewDecoder(resp.Body).Decode(&qr)
	resp.Body.Close()
	if len(qr.Hits) != 1 || qr.Hits[0].Chunk.DocID != "a" {
		t.Fatalf("hits = %+v, want top doc=a", qr.Hits)
	}

	// DELETE /doc/{id}.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/doc/a", nil)
	resp = doAuthed(t, req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	// Confirm gone via /stats.
	resp = getAuthed(t, srv.URL+"/stats")
	json.NewDecoder(resp.Body).Decode(&s)
	resp.Body.Close()
	if s.Docs != 1 {
		t.Fatalf("after delete stats.Docs = %d, want 1", s.Docs)
	}
}

func TestHTTPRejectsMalformedBody(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := postJSONAuthed(t, srv.URL+"/index", "not json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	resp2 := postJSONAuthed(t, srv.URL+"/query", `{"text":"   "}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty text status = %d, want 400", resp2.StatusCode)
	}
}

func TestHTTPDeleteWithoutID(t *testing.T) {
	srv, _ := newTestServer(t)
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/doc", nil)
	resp := doAuthed(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPluginRegistersRoutes(t *testing.T) {
	// Smoke-test the framework plugin against a freshly constructed
	// router so we know the prefix wiring works end-to-end.
	ctx := context.Background()
	_ = ctx
	idx, _ := Open(Options{Embedder: NewStubEmbedder(32)})
	p := NewPlugin(idx)
	if p.Name() != "embed" {
		t.Fatalf("Name = %q, want embed", p.Name())
	}
	if p.Index() != idx {
		t.Fatalf("Plugin.Index() returned a different index")
	}

	p2 := NewPlugin(idx).WithPrefix("/semantic")
	if p2.prefix != "/semantic" {
		t.Fatalf("WithPrefix(/semantic) -> %q", p2.prefix)
	}
	if NewPlugin(idx).WithPrefix("").prefix != "/embed" {
		t.Fatalf("WithPrefix(\"\") should fall back to /embed")
	}
}
