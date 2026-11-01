package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fallbackServe(h http.Handler, method, host, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if host != "" {
		req.Host = host
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func status(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
}

// A Group's NotFound used to run only when the Group itself was served;
// every app serves the root, so a group's 404 was silently ignored.
func TestGroupNotFoundAnswersMissesUnderItsPrefix(t *testing.T) {
	r := New()
	r.NotFound(status(404))
	api := r.Group("/api")
	api.GetFunc("/x", func(http.ResponseWriter, *http.Request) {})
	api.NotFound(status(418))
	v2 := api.Group("/v2")
	v2.NotFound(status(419))
	org := r.Group("/orgs/{org}")
	org.NotFound(status(420))

	for target, want := range map[string]int{
		"/api":            418,
		"/api/missing":    418,
		"/api/v2/missing": 419, // the longest prefix wins
		"/api/v2":         419,
		"/apix":           404, // a prefix matches whole segments only
		"/elsewhere":      404,
		"/orgs/acme/nope": 420, // a {name} segment matches any one segment
		"/orgs//nope":     404,
	} {
		if got := fallbackServe(r, http.MethodGet, "", target).Code; got != want {
			t.Errorf("GET %s = %d, want %d", target, got, want)
		}
	}
}

func TestGroupMethodNotAllowedAnswersUnderItsPrefix(t *testing.T) {
	r := New()
	api := r.Group("/api")
	api.GetFunc("/x", func(http.ResponseWriter, *http.Request) {})
	api.MethodNotAllowed(status(499))
	r.GetFunc("/y", func(http.ResponseWriter, *http.Request) {})

	rec := fallbackServe(r, http.MethodPost, "", "/api/x")
	if rec.Code != 499 || !strings.Contains(rec.Header().Get("Allow"), "GET") {
		t.Errorf("POST /api/x = %d Allow %q, want 499 with GET allowed", rec.Code, rec.Header().Get("Allow"))
	}
	if got := fallbackServe(r, http.MethodPost, "", "/y").Code; got != http.StatusMethodNotAllowed {
		t.Errorf("POST /y = %d, want the default 405 outside the group", got)
	}
}

// A Group served standalone keeps its own and its parents' handlers and
// never borrows a sibling's.
func TestGroupFallbackStaysInsideTheServedRouter(t *testing.T) {
	r := New()
	a := r.Group("/a")
	b := r.Group("/b")
	b.NotFound(status(418))
	if got := fallbackServe(a, http.MethodGet, "", "/b/missing").Code; got == 418 {
		t.Fatal("serving group /a ran sibling /b's NotFound")
	}
}

// A host group's NotFound matches the request host without its port.
func TestHostGroupNotFound(t *testing.T) {
	r := New()
	h := r.Group("api.example.com")
	h.GetFunc("/x", func(http.ResponseWriter, *http.Request) {})
	h.NotFound(status(418))
	if got := fallbackServe(r, http.MethodGet, "api.example.com:8443", "/missing").Code; got != 418 {
		t.Errorf("host group miss = %d, want 418", got)
	}
	if got := fallbackServe(r, http.MethodGet, "other.example.com", "/missing").Code; got == 418 {
		t.Error("another host ran the host group's NotFound")
	}
}

// WrapNotFound on a Group with no NotFound of its own delegates to the
// handler it would have inherited, not a bare 404.
func TestGroupWrapNotFoundDelegatesToInherited(t *testing.T) {
	r := New()
	r.NotFound(status(418))
	api := r.Group("/api")
	api.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Wrapped", "1")
			next.ServeHTTP(w, req)
		})
	})
	rec := fallbackServe(r, http.MethodGet, "", "/api/missing")
	if rec.Code != 418 || rec.Header().Get("X-Wrapped") != "1" {
		t.Errorf("GET /api/missing = %d wrapped=%q, want 418 through the wrapper", rec.Code, rec.Header().Get("X-Wrapped"))
	}
}

// Group("v1") registered host routes for Host "v1": /v1/x answered 404.
func TestGroupRefusesBarePrefix(t *testing.T) {
	for prefix, ok := range map[string]bool{
		"v1":                false,
		"api/v1":            false,
		"localhost:8080":    false, // the mux strips the port: never matches
		"api.example.com:1": false,
		"api.example.com":   true,
		"api.example.com/v": true,
		"localhost":         true,
		"/v1":               true,
		"":                  true,
	} {
		func() {
			defer func() {
				rec := recover()
				if ok && rec != nil {
					t.Errorf("Group(%q) panicked: %v", prefix, rec)
				}
				if !ok && rec == nil {
					t.Errorf("Group(%q) was accepted", prefix)
				}
			}()
			New().Group(prefix)
		}()
	}
	// Nested groups join onto a path prefix and are not checked.
	New().Group("/api").Group("v1")
}
