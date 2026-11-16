package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A screen policy's redirect hops only when the policy refused for want
// of a session. SessionPolicy says so; a policy that sends "/" to "/en"
// does not, so a cross-site visitor gets its plain 303 (issue 696).
func TestCrossSiteArrivalHopsOnlyOnSessionPolicyRedirects(t *testing.T) {
	site := app.NewApp("t")
	site.RegisterScreen(app.NewScreen("/dash", &stubComp{tag: "dash"}).WithPolicy(auth.SessionPolicy()), nil)
	site.RegisterScreen(app.NewScreen("/en", &stubComp{tag: "en"}), nil)
	site.RegisterScreen(app.NewScreen("/", &stubComp{tag: "home"}).WithPolicy(
		app.PolicyFunc(func(context.Context) app.Decision { return decide.Redirect("/en") })), nil)
	host := framework.NewUIHostApp(uihost.New(site), framework.WithoutDefaultMiddleware())
	mgr := auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: minimumNonLinkerStore{}, DevMode: true})
	h := auth.SessionMiddleware(mgr, auth.WithSessionLogger(nil))(host.Router())

	arrive := func(path, site string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Sec-Fetch-Site", site)
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := arrive("/", "cross-site"); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/en" || w.Header().Get("Refresh") != "" {
		t.Fatalf("a redirect that is no session refusal = %d Location %q Refresh %q, want the plain 303 to /en", w.Code, w.Header().Get("Location"), w.Header().Get("Refresh"))
	}
	if w := arrive("/dash?tab=2", "cross-site"); w.Code != http.StatusOK || w.Header().Get("Refresh") != "0;url=/dash?tab=2" {
		t.Fatalf("SessionPolicy's redirect = %d Location %q Refresh %q, want the hop", w.Code, w.Header().Get("Location"), w.Header().Get("Refresh"))
	}
	if w := arrive("/dash?tab=2", "same-origin"); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login?next=%2Fdash%3Ftab%3D2" {
		t.Fatalf("signed-out reload = %d Location %q, want the login redirect", w.Code, w.Header().Get("Location"))
	}
}
