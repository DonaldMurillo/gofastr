package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// mark returns middleware that appends name to X-Ran before the next
// handler runs.
func mark(name string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Ran", name)
			next.ServeHTTP(w, r)
		})
	}
}

func ran(w *httptest.ResponseRecorder) string {
	return strings.Join(w.Header().Values("X-Ran"), ",")
}

// CORS installed on a Group answers that group's preflights. A browser
// preflights a JSON POST with OPTIONS, the path has no OPTIONS route, and
// the 405 fallback ran the root chain only: the group's CORS never saw
// the request, so every preflighted call to the group was blocked while
// the POST itself carried the headers.
func TestGroupCORSAnswersPreflight(t *testing.T) {
	r := New()
	api := r.Group("/api", middleware.CORS(middleware.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}))
	api.Post("/items", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))

	req := httptest.NewRequest(http.MethodOptions, "/api/items", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight answered %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the allowed origin", got)
	}
}

// The default 405 runs the middleware every route at the path shares:
// the root's, then each enclosing group's down to the deepest group all
// of the path's routes sit under.
func TestDefault405RunsSharedGroupChain(t *testing.T) {
	r := New()
	r.Use(mark("root"))
	api := r.Group("/api", mark("api"))
	v1 := api.Group("/v1", mark("v1"))
	v1.Get("/items", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	v1.Post("/items", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/items", nil))
	if w.Code != http.StatusMethodNotAllowed || ran(w) != "root,api,v1" {
		t.Fatalf("got %d ran=%q, want 405 through root,api,v1", w.Code, ran(w))
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Fatalf("Allow = %q", got)
	}
}

// Two groups that each own one method of a path share only what is above
// both: neither group's middleware speaks for the other's route.
func TestDefault405SiblingGroupsShareOnlyTheirParent(t *testing.T) {
	r := New()
	r.Use(mark("root"))
	api := r.Group("/api", mark("api"))
	a := api.Group("", mark("a"))
	b := api.Group("", mark("b"))
	a.Get("/items", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	b.Post("/items", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/items", nil))
	if w.Code != http.StatusMethodNotAllowed || ran(w) != "root,api" {
		t.Fatalf("got %d ran=%q, want 405 through root,api only", w.Code, ran(w))
	}
}

// A custom MethodNotAllowed set on the root still answers, inside the
// group chain of the routes the path has.
func TestCustomRoot405RunsGroupChain(t *testing.T) {
	r := New()
	r.Use(mark("root"))
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("custom"))
	}))
	api := r.Group("/api", mark("api"))
	api.Post("/items", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.Get("/plain", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Body.String() != "custom" || ran(w) != "root,api" {
		t.Fatalf("got %d %q ran=%q, want the custom 405 through root,api", w.Code, w.Body.String(), ran(w))
	}
	// A root route's 405 is unchanged: the root chain, once.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/plain", nil))
	if w.Body.String() != "custom" || ran(w) != "root" {
		t.Fatalf("root route: got %q ran=%q, want custom through root", w.Body.String(), ran(w))
	}
}

// A group's own MethodNotAllowed already ran the group's chain; it runs
// it once, not twice.
func TestGroup405HandlerRunsChainOnce(t *testing.T) {
	r := New()
	api := r.Group("/api", mark("api"))
	api.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	api.Post("/items", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if w.Code != http.StatusMethodNotAllowed || ran(w) != "api" {
		t.Fatalf("got %d ran=%q, want 405 through api once", w.Code, ran(w))
	}
}
