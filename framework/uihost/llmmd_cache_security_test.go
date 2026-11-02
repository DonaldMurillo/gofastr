package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A page whose policy admits only a signed-in caller renders that
// caller's content. Its HTML answer is no-store + Vary: Cookie
// (finishPageDocument); every markdown twin ran the same policy (and the
// per-URL ones the page's Load), so they answer the same way. They sent
// no-cache with no Vary: Cookie, which a shared cache may store and
// replay to the next caller of the URL.

func varyHas(h http.Header, name string) bool {
	for _, v := range h.Values("Vary") {
		for part := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), name) {
				return true
			}
		}
	}
	return false
}

func signedInPolicy() app.Policy {
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		r := app.RequestFromContext(ctx)
		if r == nil || r.Header.Get("Cookie") != "user=alice" {
			return app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
		}
		return app.Decision{Kind: app.DecisionAllow}
	})
}

// secretThingComp is secretRenderComp on a dynamic route.
type secretThingComp struct{ secretRenderComp }

func (*secretThingComp) SetParams(map[string]string) {}

func TestPageMarkdownIsNotStorable(t *testing.T) {
	a := app.NewApp("md-cache")
	a.Register("/", &testHomeComp{}, nil)
	a.RegisterScreen(app.NewScreen("/account", &secretRenderComp{}).WithPolicy(signedInPolicy()), nil)
	a.RegisterScreen(app.NewScreen("/things/{id}", &secretThingComp{}).WithPolicy(signedInPolicy()), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation(), WithLLMsTxt("md-cache", "A test app.", nil))

	for _, tc := range []struct{ name, url, accept, want string }{
		{"html", "/account", "", "internal instructions"},
		{"negotiated", "/account", "text/markdown", "internal instructions"},
		{"exact llm.md", "/account/llm.md", "", "internal instructions"},
		{"dynamic llm.md", "/things/42/llm.md", "", "internal instructions"},
		{"pattern llm.md", "/things/:id/llm.md", "", "#"},
		{"index", "/llm-pages.md", "", "/account"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		req.Header.Set("Cookie", "user=alice")
		if tc.accept != "" {
			req.Header.Set("Accept", tc.accept)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: setup: signed-in caller got %d without %q: %.300s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
		cc := rec.Header().Get("Cache-Control")
		if !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [uihost] %s %s: per-caller body served with Cache-Control %q (storable); the HTML twin is no-store", tc.name, tc.url, cc)
		}
		if !varyHas(rec.Header(), "Cookie") {
			t.Errorf("SECURITY: [uihost] %s %s: per-caller body carries no Vary: Cookie (Vary %q)", tc.name, tc.url, rec.Header().Values("Vary"))
		}
		if tc.accept != "" && !varyHas(rec.Header(), "Accept") {
			t.Errorf("%s: negotiated answer lost Vary: Accept", tc.name)
		}
	}

	// /llms.txt runs nothing per caller: it keeps its revalidating
	// no-cache, not the per-caller no-store.
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("/llms.txt: %d Cache-Control %q, want 200 no-cache", rec.Code, rec.Header().Get("Cache-Control"))
	}
}
