//go:build chromium

package auth

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// Sign in with Apple in a real browser: the app on localhost, a fake
// Apple on 127.0.0.1 (another site), whose authorize page answers the
// way Apple does with the email scope, an auto-submitted cross-site
// form POST to the callback. The POST carries no SameSite=Lax state
// cookie; the hop page's navigation must, or the sign-in never lands.
// The app runs under WithBFFPosture, so CSRF and the origin guard are
// in the path too.
func TestAppleFormPostSignInInBrowser(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)

	apple := newFakeApple(t)
	apple.claims = apple.appleClaims("lee@example.com")
	// Apple's authorize page, on another site: it signs the user in and
	// posts the code back to redirect_uri.
	authorize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("response_mode") != "form_post" {
			http.Error(w, "response_mode must be form_post", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><form id=f method=post action="%s">
<input type=hidden name=code value="the-code"><input type=hidden name=state value="%s">
<input type=hidden name=user value='{"name":{"firstName":"Lee"}}'></form>
<script>document.getElementById('f').submit()</script>`,
			html.EscapeString(q.Get("redirect_uri")), html.EscapeString(q.Get("state")))
	}))
	t.Cleanup(authorize.Close)
	apple.endpointsOverride = map[string]string{"authorization_endpoint": authorize.URL + "/authorize"}

	provider, err := NewAppleProvider(AppleConfig{
		ClientID: apple.clientID, TeamID: "TEAM123456", KeyID: "KEY1234567",
		PrivateKey: apple.pem, RedirectURL: origin + "/auth/oauth/apple/callback",
		Issuer: apple.issuer, HTTPClient: newOIDCTestHTTPClient(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	users := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "test-secret", UserStore: users, AllowInMemoryStores: true})
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"apple": provider},
		StateSecret: "test-secret-key-state",
	}))
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{AllowedOrigins: []string{origin}}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRoutes(app.Router())
	app.Router().Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := "nobody"
		if sess, err := mgr.requestSession(r, false); err == nil {
			if u, err := users.FindByID(r.Context(), sess.UserID); err == nil {
				who = u.GetEmail()
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><p id=who>%s</p>`, html.EscapeString(who))
	}))
	srv := &http.Server{Handler: app.Router()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+"/auth/oauth/apple")); err != nil {
		t.Fatal(err)
	}
	// The trip is four navigations; an evaluate that races one of them
	// loses its context, so poll from here and retry.
	var who string
	for deadline := time.Now().Add(15 * time.Second); ; {
		var at string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(`location.pathname + '|' + (document.getElementById('who') ? document.getElementById('who').textContent : '')`, &at))
		if err == nil && len(at) > 2 && at[:2] == "/|" && at != "/|" {
			who = at[2:]
			break
		}
		if time.Now().After(deadline) {
			var body string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`location.href + ' ' + document.body.innerText`, &body))
			t.Fatalf("sign-in did not land: %v (%q)", err, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if who != "lee@example.com" {
		t.Fatalf("signed in as %q, want lee@example.com", who)
	}
	apple.checkClientSecret(t)
	if _, _, err := users.FindByEmail(context.Background(), "lee@example.com"); err != nil {
		t.Fatalf("account: %v", err)
	}
}
