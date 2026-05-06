package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helper to record middleware execution order
func trackMiddleware(name string, order *[]string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name+"-in")
			next.ServeHTTP(w, r)
			*order = append(*order, name+"-out")
		})
	}
}

func TestPathParams(t *testing.T) {
	r := New()
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := Param(req, "id")
		w.Write([]byte(id))
	}))

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "42" {
		t.Fatalf("expected body '42', got %q", w.Body.String())
	}
}

func TestMultiplePathParams(t *testing.T) {
	r := New()
	r.Get("/users/{id}/posts/{postId}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := Param(req, "id")
		postId := Param(req, "postId")
		w.Write([]byte(id + ":" + postId))
	}))

	req := httptest.NewRequest(http.MethodGet, "/users/7/posts/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "7:99" {
		t.Fatalf("expected body '7:99', got %q", w.Body.String())
	}
}

func TestParamsMap(t *testing.T) {
	r := New()
	r.Get("/users/{id}/posts/{postId}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		params := Params(req)
		w.Write([]byte(params["id"] + ":" + params["postId"]))
	}))

	req := httptest.NewRequest(http.MethodGet, "/users/7/posts/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "7:99" {
		t.Fatalf("expected body '7:99', got %q", w.Body.String())
	}
}

func TestMethodMatching(t *testing.T) {
	r := New()
	var called string

	r.Get("/users", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = "GET"
		w.WriteHeader(http.StatusOK)
	}))
	r.Post("/users", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = "POST"
		w.WriteHeader(http.StatusOK)
	}))

	// GET /users should route to GET handler
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("GET /users: expected 200, got %d", w.Code)
	}
	if called != "GET" {
		t.Fatalf("expected GET handler, got %q", called)
	}

	// POST /users should route to POST handler
	called = ""
	req = httptest.NewRequest(http.MethodPost, "/users", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("POST /users: expected 200, got %d", w.Code)
	}
	if called != "POST" {
		t.Fatalf("expected POST handler, got %q", called)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/users", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// DELETE /users should fail — only GET is registered
	req := httptest.NewRequest(http.MethodDelete, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Go 1.22 ServeMux returns 405 when method doesn't match but path does
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestGroupPrefix(t *testing.T) {
	r := New()
	api := r.Group("/api/v1")

	api.Get("/health", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("expected body 'ok', got %q", w.Body.String())
	}
}

func TestGroupInheritsMiddleware(t *testing.T) {
	r := New()
	var order []string

	r.Use(trackMiddleware("global", &order))

	api := r.Group("/api")
	api.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	expected := []string{"global-in", "handler", "global-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("expected order %v, got %v", expected, order)
		}
	}
}

func TestNestedGroupsCompose(t *testing.T) {
	r := New()
	var order []string

	r.Use(trackMiddleware("root", &order))

	api := r.Group("/api", trackMiddleware("api", &order))
	admin := api.Group("/admin", trackMiddleware("admin", &order))

	admin.Get("/dashboard", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	expected := []string{"root-in", "api-in", "admin-in", "handler", "admin-out", "api-out", "root-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("expected order %v, got %v", expected, order)
		}
	}
}

func TestNotFound(t *testing.T) {
	r := New()
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("custom 404"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if w.Body.String() != "custom 404" {
		t.Fatalf("expected body 'custom 404', got %q", w.Body.String())
	}
}

func TestNotFoundDefault(t *testing.T) {
	r := New()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMiddlewareExecutionOrder(t *testing.T) {
	r := New()
	var order []string

	r.Use(trackMiddleware("first", &order))
	r.Use(trackMiddleware("second", &order))

	r.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// first is outermost, second is next, handler is innermost
	expected := []string{"first-in", "second-in", "handler", "second-out", "first-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("expected order %v, got %v", expected, order)
		}
	}
}

func TestGroupWithParams(t *testing.T) {
	r := New()
	api := r.Group("/api")

	api.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := Param(req, "id")
		w.Write([]byte(id))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "42" {
		t.Fatalf("expected body '42', got %q", w.Body.String())
	}
}

func TestConvenienceMethods(t *testing.T) {
	r := New()
	var method string

	register := func(m string) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			method = m
			w.WriteHeader(http.StatusOK)
		}
	}

	r.Get("/g", register("GET"))
	r.Post("/p", register("POST"))
	r.Put("/u", register("PUT"))
	r.Delete("/d", register("DELETE"))
	r.Patch("/pa", register("PATCH"))

	tests := []struct {
		path   string
		method string
		want   string
	}{
		{"/g", http.MethodGet, "GET"},
		{"/p", http.MethodPost, "POST"},
		{"/u", http.MethodPut, "PUT"},
		{"/d", http.MethodDelete, "DELETE"},
		{"/pa", http.MethodPatch, "PATCH"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			method = ""
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != 200 {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if method != tt.want {
				t.Fatalf("expected method %q, got %q", tt.want, method)
			}
		})
	}
}

func TestWildcardParam(t *testing.T) {
	r := New()
	r.Get("/files/{path...}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := Param(req, "path")
		w.Write([]byte(p))
	}))

	req := httptest.NewRequest(http.MethodGet, "/files/docs/readme.md", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "docs/readme.md" {
		t.Fatalf("expected body 'docs/readme.md', got %q", w.Body.String())
	}
}

func TestHandleFunc(t *testing.T) {
	r := New()
	r.HandleFunc("GET", "/hello", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("hello"))
	})

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "hello" {
		t.Fatalf("expected body 'hello', got %q", w.Body.String())
	}
}

func TestMultipleRoutesOnGroup(t *testing.T) {
	r := New()
	api := r.Group("/api")

	var buf strings.Builder
	api.Get("/users", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		buf.WriteString("users")
	}))
	api.Get("/posts", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		buf.WriteString("posts")
	}))

	tests := []struct {
		path string
		want string
	}{
		{"/api/users", "users"},
		{"/api/posts", "posts"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			buf.Reset()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != 200 {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if buf.String() != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, buf.String())
			}
		})
	}
}

func TestGroupMiddlewareIsolation(t *testing.T) {
	r := New()
	var order []string

	r.Use(trackMiddleware("root", &order))

	api := r.Group("/api", trackMiddleware("api", &order))
	web := r.Group("/web", trackMiddleware("web", &order))

	api.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "api-handler")
	}))
	web.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "web-handler")
	}))

	// Hit /api/test — should get root + api middleware
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	expected := []string{"root-in", "api-in", "api-handler", "api-out", "root-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("expected order %v, got %v", expected, order)
		}
	}

	// Hit /web/test — should get root + web middleware
	order = order[:0]
	req = httptest.NewRequest(http.MethodGet, "/web/test", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	expected = []string{"root-in", "web-in", "web-handler", "web-out", "root-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("expected order %v, got %v", expected, order)
		}
	}
}
