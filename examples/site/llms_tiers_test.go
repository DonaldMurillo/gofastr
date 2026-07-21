package main

// Parity gates for the tiered llms.txt surface: the index lists every
// embedded doc, every indexed URL serves the real markdown, and the full
// tier carries the whole corpus. One app boot per test — the per-request
// serve() helper would boot the site once per doc.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/docs"
)

// corpusTopics returns every embedded doc name except README (the folder
// index, which is not a page).
func corpusTopics(t *testing.T) []docs.Topic {
	t.Helper()
	topics, err := docs.List()
	if err != nil {
		t.Fatal(err)
	}
	out := topics[:0]
	for _, tp := range topics {
		if tp.Name != "README" {
			out = append(out, tp)
		}
	}
	if len(out) == 0 {
		t.Fatal("embedded docs corpus is empty")
	}
	return out
}

func TestLLMsTxtIndexesEveryDoc(t *testing.T) {
	index := body(t, "/llms.txt")
	for _, tp := range corpusTopics(t) {
		if !strings.Contains(index, "(/docs/"+tp.Name+".md)") {
			t.Errorf("/llms.txt does not index /docs/%s.md", tp.Name)
		}
	}
	if !strings.Contains(index, "(/llms-full.txt)") {
		t.Error("/llms.txt does not link the full-corpus tier")
	}
}

func TestDocMarkdownRoutesServeRawDocs(t *testing.T) {
	app := newTestApp(t)
	for _, tp := range corpusTopics(t) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/docs/"+tp.Name+".md", nil)
		req.Host = "localhost:8083"
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /docs/%s.md = %d, want 200", tp.Name, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
			t.Errorf("/docs/%s.md Content-Type = %q, want text/markdown", tp.Name, ct)
		}
		want, err := docs.Get(tp.Name)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Body.String() != string(want) {
			t.Errorf("/docs/%s.md did not serve the embedded markdown verbatim", tp.Name)
		}
	}
}

func TestLLMsFullTxtCarriesTheCorpus(t *testing.T) {
	full := body(t, "/llms-full.txt")
	for _, tp := range corpusTopics(t) {
		if !strings.Contains(full, "<!-- /docs/"+tp.Name+".md -->") {
			t.Errorf("/llms-full.txt is missing doc %s", tp.Name)
		}
	}
}

func TestDocMarkdownUnknownTopic404(t *testing.T) {
	rec := serve(t, http.MethodGet, "/docs/no-such-doc.md")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /docs/no-such-doc.md = %d, want 404", rec.Code)
	}
}
