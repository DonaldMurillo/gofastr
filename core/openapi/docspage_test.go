package openapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
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
		{BasePath: "javascript:"},
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
