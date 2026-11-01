package openapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func docsSpec() *Spec {
	s := NewSpec("Orders", "2.1.0")
	s.SetDescription("Order intake.\n\nSecond paragraph.")
	get := NewOperation()
	get.Summary = "List orders"
	get.Tags = []string{"orders"}
	s.AddPath("GET", "/orders", *get)
	post := NewOperation()
	post.Summary = "Create <script>alert(1)</script>"
	post.Tags = []string{"orders"}
	s.AddPath("POST", "/orders", *post)
	ping := NewOperation()
	ping.Summary = "Health"
	s.AddPath("GET", "/ping", *ping)
	return s
}

func serveDocs(h http.Handler, target string, authed bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if authed {
		r = r.WithContext(handler.SetUser(r.Context(), "u1"))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// The #635 layout: spec at /swagger/v1/swagger.json, page at
// /swagger/ and /swagger/index.html; nothing else answers.
func TestDocsHandlerWithCustomPaths(t *testing.T) {
	h := DocsHandlerWith(docsSpec(), DocsConfig{BasePath: "/swagger", SpecPath: "/v1/swagger.json", PagePath: "/index.html", Public: true})
	if rec := serveDocs(h, "/swagger/v1/swagger.json", false); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"openapi": "3.1.0"`) {
		t.Fatalf("spec = %d %q", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/swagger/", "/swagger/index.html"} {
		rec := serveDocs(h, p, false)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `href="/swagger/v1/swagger.json"`) {
			t.Fatalf("page at %s = %d; want 200 linking the spec: %s", p, rec.Code, rec.Body.String())
		}
	}
	for _, p := range []string{"/swagger/openapi.json", "/swagger/other", "/swagger/v1/"} {
		if rec := serveDocs(h, p, false); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d; want 404", p, rec.Code)
		}
	}
}

// The page lists the operations, grouped by tag with untagged last,
// with every spec value escaped.
func TestDocsPageListsOperations(t *testing.T) {
	body := serveDocs(DocsHandlerWith(docsSpec(), DocsConfig{BasePath: "/docs", Public: true}), "/docs/", false).Body.String()
	for _, want := range []string{
		"<h1>Orders — API reference</h1>", "Version 2.1.0", "<p>Order intake.</p>", "<p>Second paragraph.</p>",
		`<h2 id="ops-0">orders</h2>`, "<code>GET</code></td><td><code>/orders</code></td><td>List orders</td>",
		"Create &lt;script&gt;alert(1)&lt;/script&gt;", `<h2 id="ops-1">Other operations</h2>`, "<code>/ping</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Fatal("a spec value reached the page unescaped")
	}
	if strings.Index(body, "Other operations") < strings.Index(body, ">orders<") {
		t.Fatal("untagged operations must come after the tagged groups")
	}
}

// The page needs nothing inline, so it renders under the default
// Content-Security-Policy: no <script>, no <style>, no style attribute,
// and its own policy allows no inline source.
func TestDocsPageIsCSPClean(t *testing.T) {
	rec := serveDocs(DocsHandlerWith(docsSpec(), DocsConfig{BasePath: "/docs", Public: true}), "/docs/", false)
	body := strings.ToLower(rec.Body.String())
	for _, bad := range []string{"<script", "<style", "style=", "<link"} {
		if strings.Contains(body, bad) {
			t.Errorf("page carries %q", bad)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("CSP = %q; want a policy with no unsafe-inline", csp)
	}
}

// The gated page lists what the caller's spec view holds, not the
// whole spec.
func TestDocsPageGatedUsesRequestView(t *testing.T) {
	s := docsSpec()
	s.RequestView = func(*http.Request) *Spec {
		v := NewSpec("Orders", "2.1.0")
		op := NewOperation()
		op.Summary = "Health"
		v.AddPath("GET", "/ping", *op)
		return v
	}
	h := DocsHandlerWith(s, DocsConfig{BasePath: "/docs"})
	if rec := serveDocs(h, "/docs/", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous gated page = %d; want 401", rec.Code)
	}
	body := serveDocs(h, "/docs/", true).Body.String()
	if strings.Contains(body, "/orders") || !strings.Contains(body, "/ping") {
		t.Fatalf("gated page listed outside the caller's view: %s", body)
	}
	if !strings.Contains(body, "<p>Order intake.</p>") {
		t.Fatal("the gated page lost the spec description")
	}
}

func TestDocsHandlerWithRefusesBadPaths(t *testing.T) {
	for _, cfg := range []DocsConfig{
		{BasePath: "/docs", SpecPath: "openapi.json"},
		{BasePath: "/docs", SpecPath: "/a/../b"},
		{BasePath: "/docs", SpecPath: "/a b"},
		{BasePath: "/docs", PagePath: "/openapi.json"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("DocsHandlerWith accepted %+v", cfg)
				}
			}()
			DocsHandlerWith(NewSpec("t", "1"), cfg)
		}()
	}
}

// "/docs/" is the same mount as "/docs": the slash must not send the
// page or the spec outside it.
func TestDocsHandlerWithTrailingSlashBasePath(t *testing.T) {
	h := DocsHandlerWith(docsSpec(), DocsConfig{BasePath: "/docs/", Public: true})
	for _, p := range []string{"/docs/", "/docs", "/docs/openapi.json"} {
		if rec := serveDocs(h, p, false); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d (Location %q); want 200", p, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// Unknown paths and methods answer through i18n like the framework's
// other refusals, not with net/http's fixed English text: under a French
// catalog the body is French.
func TestDocsHandlerWithRefusalsAreI18n(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", i18n.StatusPhrase(http.StatusNotFound).Key(), i18n.Message{Text: "introuvable"})
	cat.Set("fr", i18n.StatusPhrase(http.StatusMethodNotAllowed).Key(), i18n.Message{Text: "méthode refusée"})
	fr := func(r *http.Request) *http.Request {
		ctx := i18n.WithTranslator(i18n.WithContext(r.Context(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(cat, "en"))
		return r.WithContext(ctx)
	}
	h := DocsHandlerWith(docsSpec(), DocsConfig{BasePath: "/docs", Public: true})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, fr(httptest.NewRequest(http.MethodGet, "/docs/nope", nil)))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "introuvable") {
		t.Fatalf("unknown path = %d %q; want the translated 404", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, fr(httptest.NewRequest(http.MethodPost, "/docs/openapi.json", nil)))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" || !strings.Contains(rec.Body.String(), "méthode refusée") {
		t.Fatalf("POST = %d %q allow=%q; want the translated 405 with Allow", rec.Code, rec.Body.String(), rec.Header().Get("Allow"))
	}
	if rec := serveDocs(h, "/docs/openapi.json", false); rec.Code != http.StatusOK {
		t.Fatalf("spec = %d", rec.Code)
	}
}

// A mount at the site root serves the page at / and the spec at
// /openapi.json.
func TestDocsHandlerWithRootBasePath(t *testing.T) {
	h := DocsHandlerWith(docsSpec(), DocsConfig{BasePath: "/", Public: true})
	for _, p := range []string{"/", "/openapi.json"} {
		if rec := serveDocs(h, p, false); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d; want 200", p, rec.Code)
		}
	}
	if body := serveDocs(h, "/", false).Body.String(); !strings.Contains(body, `href="/openapi.json"`) {
		t.Fatalf("root-mounted page does not link /openapi.json: %s", body)
	}
}

// A RequestView may hand back a spec it shares between requests (one
// per role, say); serving must not write into it.
func TestRequestViewSpecIsNotMutated(t *testing.T) {
	s := NewSpec("Orders", "2.1.0")
	s.SetDescription("Order intake.")
	shared := NewSpec("Orders", "2.1.0")
	s.RequestView = func(*http.Request) *Spec { return shared }
	serveDocs(DocsHandlerWith(s, DocsConfig{BasePath: "/docs"}), "/docs/", true)
	serveDocs(Handler(s), "/", true)
	if _, ok := shared.info["description"]; ok {
		t.Fatal("serving wrote the base spec's description into the shared RequestView spec")
	}
}
