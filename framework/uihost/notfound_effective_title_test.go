package uihost

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type notFoundTitleContextKey struct{}

type nfPathTitle struct {
	value      string
	panicTitle bool
}

func (n nfPathTitle) Render() render.HTML { return "unavailable" }
func (n nfPathTitle) NotFoundTitle(ctx context.Context, path string) string {
	if n.panicTitle {
		panic("title failed")
	}
	if path == "" {
		return "Missing outlet"
	}
	if n.value == "by-path" {
		return ctx.Value(notFoundTitleContextKey{}).(string) + path
	}
	return n.value
}

type nfStaticTitle struct{}

func (nfStaticTitle) Render() render.HTML                { return "unavailable" }
func (nfStaticTitle) ScreenTitle(context.Context) string { return "Static unavailable" }

type nfBothTitles struct{ nfStaticTitle }

func (nfBothTitles) NotFoundTitle(context.Context, string) string { return "" }

func TestNotFoundEffectiveTitles(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp component.Component
		want string
	}{
		{"default", nil, "Page not found"},
		{"path", nfPathTitle{value: "Task unavailable"}, "Task unavailable"},
		{"escaped", nfPathTitle{value: `Notes <private> & unavailable`}, `Notes <private> & unavailable`},
		{"empty", nfPathTitle{}, "Page not found"},
		{"static", nfStaticTitle{}, "Static unavailable"},
		{"empty-path-fallback", nfBothTitles{}, "Static unavailable"},
		{"panic", nfPathTitle{panicTitle: true}, "Page not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := app.NewApp("Title test")
			a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
			a.RegisterScreen(app.NewScreen("/origin", statusScreenComp("origin")).WithTitle("Origin secret"), nil)
			key := app.NewKey[string]("not-found-title")
			a.RegisterScreen(app.NewScreen("/denied", statusScreenComp("never render")).WithTitle("Private denied route").Resolve(key.From(func(context.Context) (string, error) { return "", app.ErrNotFound })).WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
				_, _ = key.Get(ctx)
				return app.Decision{Kind: app.DecisionAllow}
			})), nil)
			host := New(a)
			if tc.comp != nil {
				WithNotFoundScreen(tc.comp)(host)
			}
			for _, path := range []string{"/missing", "/denied"} {
				for _, partial := range []bool{false, true} {
					req := httptest.NewRequest(http.MethodGet, path, nil)
					if partial {
						req.Header.Set("X-Gofastr-Navigate", "1")
						req.Header.Set("X-Gofastr-From", "/origin")
					}
					res := httptest.NewRecorder()
					host.ServeHTTP(res, req)
					want := tc.want + " — Title test"
					if res.Code != 404 {
						t.Fatalf("%s status=%d", path, res.Code)
					}
					if partial {
						got, err := url.PathUnescape(res.Header().Get("X-Gofastr-Title"))
						if err != nil || got != want {
							t.Errorf("%s partial=%q err=%v want=%q", path, got, err, want)
						}
					} else if !strings.Contains(res.Body.String(), "<title>"+html.EscapeString(want)+"</title>") {
						t.Errorf("%s full title missing %q", path, want)
					}
				}
			}
		})
	}
}

func TestNotFoundTitleUsesRequestContextAndPath(t *testing.T) {
	a := app.NewApp("Title test")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	a.RegisterScreen(app.NewScreen("/origin", statusScreenComp("origin")), nil)
	host := New(a, WithNotFoundScreen(nfPathTitle{value: "by-path"}))
	for _, path := range []string{"/first", "/second"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = req.WithContext(context.WithValue(req.Context(), notFoundTitleContextKey{}, "Current "))
			req.Header.Set("X-Gofastr-Navigate", "1")
			req.Header.Set("X-Gofastr-From", "/origin")
			res := httptest.NewRecorder()
			host.ServeHTTP(res, req)
			got, _ := url.PathUnescape(res.Header().Get("X-Gofastr-Title"))
			if res.Code != 404 || got != "Current "+path+" — Title test" {
				t.Fatal("request title", res.Code, got)
			}
		})
	}
}

func TestNotFoundTitleOutletKeepsEmptyPath(t *testing.T) {
	host := New(outlet404App(), WithNotFoundScreen(nfPathTitle{value: "not an outlet"}))
	for _, partial := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/missing", nil)
		if partial {
			req.Header.Set("X-Gofastr-Navigate", "1")
			req.Header.Set("X-Gofastr-From", "/present")
		}
		res := httptest.NewRecorder()
		host.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("partial=%v status=%d", partial, res.Code)
		}
		const want = "Missing outlet — nf"
		if partial {
			got, err := url.PathUnescape(res.Header().Get("X-Gofastr-Title"))
			if err != nil || got != want {
				t.Fatalf("partial title=%q err=%v want=%q", got, err, want)
			}
		} else if !strings.Contains(res.Body.String(), "<title>"+want+"</title>") {
			t.Fatalf("full outlet title missing %q", want)
		}
	}
}

func TestNotFoundTitlePreservesUnmatchedPathWithMissingOutlet(t *testing.T) {
	host := New(outlet404App(), WithNotFoundScreen(nfPathTitle{value: "by-path"}))
	for _, partial := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/unmatched", nil)
		req = req.WithContext(context.WithValue(req.Context(), notFoundTitleContextKey{}, "Unmatched "))
		if partial {
			req.Header.Set("X-Gofastr-Navigate", "1")
			req.Header.Set("X-Gofastr-From", "/present")
		}
		res := httptest.NewRecorder()
		host.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("partial=%v status=%d", partial, res.Code)
		}
		const want = "Unmatched /unmatched — nf"
		if partial {
			got, err := url.PathUnescape(res.Header().Get("X-Gofastr-Title"))
			if err != nil || got != want {
				t.Errorf("partial title=%q err=%v want=%q", got, err, want)
			}
		} else if !strings.Contains(res.Body.String(), "<title>"+want+"</title>") {
			t.Errorf("full title lost unmatched request path: want=%q", want)
		}
	}
}
