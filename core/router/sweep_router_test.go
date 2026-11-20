package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSweepRouterUseAfterFirstRequestRecomposes(t *testing.T) {
	r := New()
	g := r.Group("/api")
	g.Get("/x", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x", nil))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Late", "1")
			next.ServeHTTP(w, req)
		})
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if rec.Header().Get("X-Late") != "1" {
		t.Fatal("middleware added after a route served a request did not wrap it")
	}
}

func TestSweepRouterHooksSetOnGroupGovernTree(t *testing.T) {
	r := New()
	g := r.Group("/api")
	var registered []string
	g.SetRegisterHook(func(m, p string) { registered = append(registered, m+" "+p) })
	g.SetRouteGate(func(key string) bool { return key != "GET /gated" })
	r.Get("/gated", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	if len(registered) != 1 || registered[0] != "GET /gated" {
		t.Fatalf("register hook set on a Group saw %v, want [GET /gated]", registered)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/gated", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("gate set on a Group: root route answered %d, want 404", rec.Code)
	}
	var served string
	g.SetServeHook(func(m, p string) { served = m + " " + p })
	g.PreRoute(func(req *http.Request) (*http.Request, string) {
		if req.URL.Path != "/alias" {
			return nil, ""
		}
		next := req.Clone(req.Context())
		next.URL.Path = "/users/7"
		return next, ""
	})
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/alias", nil))
	if rec.Code != http.StatusOK || served != "GET /users/{id}" {
		t.Fatalf("hooks set on a Group: root answered %d, serve hook saw %q; want 200 and GET /users/{id}", rec.Code, served)
	}
}

func TestSweepRouterServeHookOrder(t *testing.T) {
	r := New()
	var order []string
	r.SetServeHook(func(m, p string) { order = append(order, "hook:"+m+" "+p) })
	r.SetRouteGate(func(key string) bool { order = append(order, "gate"); return key != "GET /off" })
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "chain")
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	r.Get("/off", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/off", nil))
	got := strings.Join(order, ",")
	if want := "gate,hook:GET /users/{id},chain,gate"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
}

func TestSweepRouterParamSingleSegmentStopsAtSlash(t *testing.T) {
	r := New()
	var got string
	r.Get("/f/{name}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = Param(req, "name")
	}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/f/a%2Fb", nil))
	if got != "a" {
		t.Fatalf("Param = %q, want %q", got, "a")
	}
}
