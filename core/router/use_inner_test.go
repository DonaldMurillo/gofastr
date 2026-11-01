package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func traceMW(name string, trace *[]string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*trace = append(*trace, name)
			next.ServeHTTP(w, r)
		})
	}
}

// UseInner middleware runs after every Use and Group middleware, even one
// added later, and a call on a Group reaches the whole tree.
func TestUseInnerRunsAfterUseAndGroup(t *testing.T) {
	var trace []string
	r := New()
	r.UseInner(traceMW("inner1", &trace))
	g := r.Group("/api", traceMW("group", &trace))
	g.Get("/x", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		trace = append(trace, "handler")
	}))
	r.Get("/y", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		trace = append(trace, "handler")
	}))
	r.Use(traceMW("outer", &trace))
	g.UseInner(traceMW("inner2", &trace))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if got, want := strings.Join(trace, ","), "outer,group,inner1,inner2,handler"; got != want {
		t.Fatalf("group route order = %s, want %s", got, want)
	}
	trace = nil
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/y", nil))
	if got, want := strings.Join(trace, ","), "outer,inner1,inner2,handler"; got != want {
		t.Fatalf("root route order = %s, want %s", got, want)
	}
}
