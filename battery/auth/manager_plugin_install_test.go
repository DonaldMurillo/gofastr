package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// New deliberately installs NO plugin: AuthManager starts empty and
// RegisterRoutes iterates only plugins added with Use. Pin that contract
// so a future change cannot silently re-introduce the old (and formerly
// godoc-promised) "core plugin is always loaded first" behaviour, which
// would mount login/logout/me/register routes the host never asked for.
//
// Before any Use call: no "core" plugin and GET /auth/me is a 404 (the
// router never had the route). After Use(NewCorePlugin()): the plugin is
// registered and GET /auth/me is a 401 (the handler ran, found no
// session) — i.e. the route exists.
func TestNew_NoCoreRoutesUntilUse(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true})

	if _, ok := mgr.Plugin("core"); ok {
		t.Fatal("New must not install the core plugin; Plugin(\"core\") should be absent until Use")
	}

	// No plugin registered yet → no routes mounted.
	r := router.New()
	mgr.RegisterRoutes(r)
	if code := serveCode(r, http.MethodGet, "/auth/me"); code != http.StatusNotFound {
		t.Fatalf("before Use, GET /auth/me must be 404 (route absent); got %d", code)
	}

	// Install the core plugin exactly as the corrected godoc shows.
	mgr.Use(NewCorePlugin())
	if _, ok := mgr.Plugin("core"); !ok {
		t.Fatal("Use(NewCorePlugin()) must register the core plugin")
	}

	// Init the plugin (sets its mgr ref) and register routes on a fresh
	// router, mirroring how a host mounts auth manually.
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init after Use: %v", err)
	}
	r2 := router.New()
	mgr.RegisterRoutes(r2)
	if code := serveCode(r2, http.MethodGet, "/auth/me"); code != http.StatusUnauthorized {
		t.Fatalf("after Use, GET /auth/me must be 401 (route present, no session); got %d", code)
	}
}

func serveCode(handler http.Handler, method, path string) int {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code
}
