package webmcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func manifestTools(t *testing.T, rt *router.Router) []Tool {
	t.Helper()
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ManifestRoute, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest route: %d", rec.Code)
	}
	var m struct{ Tools []Tool }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest not JSON: %v", err)
	}
	return m.Tools
}

func TestHandleBindsDeclarationAndRoute(t *testing.T) {
	h := New()
	rt := router.New()
	handled := ""
	if err := h.Handle(rt, Tool{
		Name:        "create_note",
		Description: "Creates a note.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"body":{"type":"string"}},"required":["body"]}`),
		Method:      http.MethodPost,
		Path:        "/api/notes",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		handled = in.Body
		_, _ = w.Write([]byte(`{"ok":true}`))
	})); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}

	// The route serves a typed JSON mutation body, callable by hand —
	// a WebMCP call differs only in the marker header.
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(`{"body":"hi"}`)))
	if rec.Code != http.StatusOK || handled != "hi" {
		t.Fatalf("route call: code=%d handled=%q", rec.Code, handled)
	}

	// The same declaration is the manifest entry: one source of truth.
	tools := manifestTools(t, rt)
	if len(tools) != 1 || tools[0].Name != "create_note" ||
		tools[0].Method != http.MethodPost || tools[0].Path != "/api/notes" {
		t.Fatalf("manifest disagrees with the bound route: %+v", tools)
	}
}

func TestHandleGETBindsQueryParams(t *testing.T) {
	h := New()
	rt := router.New()
	if err := h.Handle(rt, Tool{
		Name:        "search",
		Description: "Searches.",
		Method:      http.MethodGet,
		// The query is part of the declaration (the bridge bakes it in)
		// but the route pattern is the bare path; input keys ride along
		// as query params the handler reads normally.
		Path: "/api/search?source=baked",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"q":%q,"source":%q}`, r.URL.Query().Get("q"), r.URL.Query().Get("source"))
	})); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=gopher&source=agent", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("query-bound route: %d", rec.Code)
	}
	if got := rec.Body.String(); got != `{"q":"gopher","source":"agent"}` {
		t.Fatalf("query binding: %s", got)
	}
}

func TestHandleAppliesHTTPMiddlewareOutermostFirst(t *testing.T) {
	h := New()
	rt := router.New()
	var order []string
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Role") != "support" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			order = append(order, "auth")
			next.ServeHTTP(w, r)
		})
	}
	log := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "log")
			next.ServeHTTP(w, r)
		})
	}
	if err := h.Handle(rt, validTool(),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
		}),
		WithHTTPMiddleware(auth, log)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set("X-Role", "support")
	rt.ServeHTTP(rec, req)
	if strings.Join(order, ",") != "auth,log,handler" {
		t.Fatalf("middleware order: %v", order)
	}

	// A rejected caller never reaches the handler: authorization stays
	// an explicit decision at the declaration site.
	order = nil
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/echo", nil))
	if rec.Code != http.StatusForbidden || len(order) != 0 {
		t.Fatalf("unauthorized call: code=%d order=%v", rec.Code, order)
	}
}

func TestHandleRejectsDuplicateRoute(t *testing.T) {
	h := New()
	rt := router.New()
	mk := func(name string) Tool {
		tl := validTool()
		tl.Name = name
		return tl
	}
	if err := h.Handle(rt, mk("echo_a"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	err := h.Handle(rt, mk("echo_b"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate route accepted: %v", err)
	}
	// The failed Handle registered neither manifest entry nor route:
	// mounting leaves exactly one tool.
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	if tools := manifestTools(t, rt); len(tools) != 1 || tools[0].Name != "echo_a" {
		t.Fatalf("failed Handle leaked into the manifest: %+v", tools)
	}
}

func TestHandleRejectsExistingRouterRoute(t *testing.T) {
	h := New()
	rt := router.New()
	rt.Post("/api/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("conflict with an app route accepted: %v", err)
	}
	// Still mountable once the collision is resolved elsewhere.
	tl := validTool()
	tl.Path = "/api/echo2"
	if err := h.Register(tl); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatalf("mount after refused handle: %v", err)
	}
	if tools := manifestTools(t, rt); len(tools) != 1 || tools[0].Path != "/api/echo2" {
		t.Fatalf("manifest after refused handle: %+v", tools)
	}
}

func TestHandleRejectsDuplicateToolName(t *testing.T) {
	h := New()
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); err != nil {
		t.Fatal(err)
	}
	// Different path, same name: still one tool per name per page.
	tl := validTool()
	tl.Path = "/api/echo2"
	err := h.Handle(rt, tl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate name accepted: %v", err)
	}
	// And the refused call registered no route for its path.
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/echo2", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("refused handle left a route behind: %d", rec.Code)
	}
}

func TestHandleRefusedAfterMount(t *testing.T) {
	h := New()
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	tl := validTool()
	tl.Name = "late"
	tl.Path = "/api/late"
	err := h.Handle(rt, tl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err == nil || !strings.Contains(err.Error(), "froze") {
		t.Fatalf("post-mount handle accepted: %v", err)
	}
}

func TestHandleOnGroupedRouterKeepsPrefix(t *testing.T) {
	h := New()
	root := router.New()
	grp := root.Group("/v1")
	if err := h.Handle(grp, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(grp, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1"+ManifestRoute, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest route: %d", rec.Code)
	}
	var m struct{ Tools []Tool }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest not JSON: %v", err)
	}
	if m.Tools[0].Path != "/api/echo" {
		t.Fatalf("grouped manifest path: %+v", m.Tools[0])
	}
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/api/echo", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("grouped route: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandleConditionalRegistrationIsAtomic(t *testing.T) {
	// The conditional-diagnostics shape: a flag gates one Handle call,
	// and whatever happens inside leaves both halves together.
	for _, enabled := range []bool{false, true} {
		h := New()
		rt := router.New()
		if err := h.Register(func() Tool {
			tl := validTool()
			tl.Name = "baseline"
			tl.Path = "/api/baseline"
			return tl
		}()); err != nil {
			t.Fatal(err)
		}
		if enabled {
			if err := h.Handle(rt, Tool{
				Name:        "diag_ping",
				Description: "Diagnostics ping.",
				Method:      http.MethodGet,
				Path:        "/api/diag",
			}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := h.Mount(rt, nil); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/diag", nil))
		tools := manifestTools(t, rt)
		if enabled {
			if rec.Code != http.StatusOK || len(tools) != 2 {
				t.Fatalf("enabled: route=%d tools=%d", rec.Code, len(tools))
			}
		} else {
			if rec.Code != http.StatusNotFound || len(tools) != 1 || tools[0].Name != "baseline" {
				t.Fatalf("disabled: route=%d tools=%+v", rec.Code, tools)
			}
		}
	}
}
