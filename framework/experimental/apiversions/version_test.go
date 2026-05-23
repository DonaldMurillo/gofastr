package apiversions_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"
)

func TestVersionNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"v1", "v1"},
		{"/v1", "v1"},
		{"1", "v1"},
		{"v2", "v2"},
		{"2", "v2"},
	}
	for _, tt := range tests {
		v := apiversions.Version(router.New(), tt.in)
		if v.Prefix() != tt.want {
			t.Errorf("Version(%q).Prefix() = %q, want %q", tt.in, v.Prefix(), tt.want)
		}
	}
}

func TestVersionFullPrefix(t *testing.T) {
	v := apiversions.Version(router.New(), "v1")
	if v.FullPrefix() != "/v1" {
		t.Errorf("FullPrefix() = %q, want %q", v.FullPrefix(), "/v1")
	}
}

func TestVersionRoutesReachable(t *testing.T) {
	r := router.New()
	v1 := apiversions.Version(r, "v1")

	called := false
	v1.Router().Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !called {
		t.Error("handler not called for /v1/ping")
	}
}

func TestDeprecationMiddleware(t *testing.T) {
	r := router.New()
	sunset := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	v1 := apiversions.Version(r, "v1",
		apiversions.WithDeprecation(sunset, "/v2"),
	)

	v1.Use(v1.DeprecationMiddleware())

	v1.Router().Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Header().Get("Deprecation") != "true" {
		t.Error("missing Deprecation: true header")
	}
	if rec.Header().Get("Sunset") == "" {
		t.Error("missing Sunset header")
	}
	link := rec.Header().Get("Link")
	if link != "</v2>; rel=\"successor-version\"" {
		t.Errorf("Link = %q, want %q", link, "</v2>; rel=\"successor-version\"")
	}
}

func TestVersionIsDeprecated(t *testing.T) {
	r := router.New()

	v1 := apiversions.Version(r, "v1")
	if v1.IsDeprecated() {
		t.Error("v1 should not be deprecated by default")
	}

	sunset := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	v2 := apiversions.Version(r, "v2",
		apiversions.WithDeprecation(sunset, "/v3"),
	)
	if !v2.IsDeprecated() {
		t.Error("v2 should be deprecated")
	}
}

func TestDeprecationHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	sunset := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	apiversions.DeprecationHeaders(rec, sunset, "/v2")

	if rec.Header().Get("Deprecation") != "true" {
		t.Error("missing Deprecation header")
	}
	if rec.Header().Get("Sunset") == "" {
		t.Error("missing Sunset header")
	}
}

func TestMultipleVersions(t *testing.T) {
	r := router.New()

	v1 := apiversions.Version(r, "v1")
	v2 := apiversions.Version(r, "v2")

	v1Called := false
	v2Called := false

	v1.Router().Get("/orders", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		v1Called = true
		w.Write([]byte("v1 orders"))
	}))
	v2.Router().Get("/orders", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		v2Called = true
		w.Write([]byte("v2 orders"))
	}))

	// Test v1
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/v1/orders", nil))
	if !v1Called {
		t.Error("v1 handler not called")
	}

	// Test v2
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/v2/orders", nil))
	if !v2Called {
		t.Error("v2 handler not called")
	}
}

func TestProjectionSet(t *testing.T) {
	ps := apiversions.NewProjectionSet(
		&apiversions.Projection{
			Version: "v1",
			Include: []string{"id", "name"},
		},
		&apiversions.Projection{
			Version: "v2",
			Exclude: []string{"internal_notes"},
		},
	)

	p1 := ps.For("v1")
	if p1 == nil {
		t.Fatal("v1 projection is nil")
	}
	if len(p1.Include) != 2 {
		t.Errorf("v1 Include = %v, want 2 items", p1.Include)
	}

	p2 := ps.For("v2")
	if p2 == nil {
		t.Fatal("v2 projection is nil")
	}

	// Unknown version returns nil (no default set)
	if ps.For("v3") != nil {
		t.Error("v3 should return nil when no default")
	}
}

func TestVersionRejectsGarbage(t *testing.T) {
	bad := []string{"", "v1/admin", "v", "v1.2.3", "vabc", "v1-beta", "/"}
	for _, in := range bad {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Version(%q) did not panic", in)
				}
			}()
			_ = apiversions.Version(router.New(), in)
		})
	}
}

func TestProjectionSetDefault(t *testing.T) {
	ps := &apiversions.ProjectionSet{
		Default: &apiversions.Projection{
			Version: "default",
			Include: []string{"id"},
		},
		Versions: map[string]*apiversions.Projection{},
	}

	p := ps.For("v99")
	if p == nil {
		t.Fatal("should fall back to default")
	}
	if p.Version != "default" {
		t.Errorf("got version %q, want %q", p.Version, "default")
	}
}
