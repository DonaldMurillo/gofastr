//go:build chromium

package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// The browser module drives both ceremonies against the plugin's routes
// with Chrome's virtual authenticator: add a passkey while signed in,
// sign in with it after the session is gone, then remove it. This is
// the wire shape a real browser sends, not one shaped by hand.
func TestPasskeyBrowserRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// WebAuthn refuses an IP address as the RP ID; localhost is a
	// secure context over plain HTTP.
	origin := fmt.Sprintf("http://localhost:%d", port)
	f := newPasskeyFixtureFor(t, "localhost", origin)

	mux := http.NewServeMux()
	mux.Handle("/auth/", f.r)
	mux.HandleFunc("/passkey.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, passkeyJS)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("as") == "alice" {
			mintSessionCookie(w, f.mgr.Config(), f.session(t, "u-alice"), time.Now().Add(time.Hour))
		}
		var del strings.Builder
		creds, _ := f.pk.store.ListPasskeys(r.Context(), "u-alice")
		for _, c := range creds {
			fmt.Fprintf(&del, `<button id="del" data-passkey-delete="/auth" data-passkey-id="%s" hidden>Remove</button>`, PasskeyID(c.ID))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The runtime's navigate and toast are stubbed: the test reads
		// what the module asked them for.
		fmt.Fprintf(w, `<!doctype html><html><meta charset=utf-8><meta name="csrf-token" content="%s">
<script>window.navs = []; window.toasts = [];
window.__gofastr = {navigate: function (p) { window.navs.push(p); }, invalidate: function () {},
  _toastOrFallback: function (c) { window.toasts.push(c.title); }};</script>
<form><input data-passkey-name value="Laptop">
<button id="add" data-passkey-register="/auth" hidden>Add a passkey</button></form>
<button id="in" data-passkey-login="/auth" hidden>Sign in with a passkey</button>
<button id="evil" data-passkey-login="//evil.example" hidden>Elsewhere</button>
%s<script src="/passkey.js"></script>`, CSRFToken(r), del.String())
	})
	// CSRF is mounted the way WithBFFPosture mounts it, so every
	// POST the module makes has to carry the page's token.
	srv := &http.Server{Handler: CSRF()(mux)}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	ctx := passkeyBrowser(t)
	if err := chromedp.Run(ctx, webauthn.Enable(), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := webauthn.AddVirtualAuthenticator(&webauthn.VirtualAuthenticatorOptions{
			Protocol: webauthn.AuthenticatorProtocolCtap2, Ctap2version: webauthn.Ctap2versionCtap21,
			Transport: webauthn.AuthenticatorTransportInternal, HasResidentKey: true,
			HasUserVerification: true, IsUserVerified: true, AutomaticPresenceSimulation: true,
		}).Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("virtual authenticator: %v", err)
	}

	wait := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var shown, evilHidden bool
	var toasts []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/?as=alice"),
		wait(`!!window.__gofastr.loadedModules.passkey`),
		chromedp.Evaluate(`!document.getElementById('add').hidden && !document.getElementById('in').hidden`, &shown),
		chromedp.Evaluate(`document.getElementById('evil').hidden`, &evilHidden),
		chromedp.Click(`#add`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
		chromedp.Evaluate(`window.toasts`, &toasts),
	); err != nil {
		t.Fatalf("register: %v (toasts %v)", err, toasts)
	}
	if !shown {
		t.Error("the passkey buttons stayed hidden in a browser with WebAuthn")
	}
	if !evilHidden {
		t.Error("a button aimed at another origin was revealed")
	}
	creds, err := f.pk.store.ListPasskeys(context.Background(), "u-alice")
	if err != nil || len(creds) != 1 || creds[0].Name != "Laptop" {
		t.Fatalf("after register: %+v, %v; want one passkey named Laptop", creds, err)
	}

	var navs []string
	if err := chromedp.Run(ctx,
		network.ClearBrowserCookies(),
		chromedp.Navigate(origin+"/"),
		wait(`!!window.__gofastr.loadedModules.passkey`),
		chromedp.Click(`#in`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
		chromedp.Evaluate(`window.navs`, &navs),
		chromedp.Evaluate(`window.toasts`, &toasts),
	); err != nil {
		t.Fatalf("sign in: %v (toasts %v)", err, toasts)
	}
	if len(navs) != 1 || navs[0] != "/app" {
		t.Errorf("sign-in navigated to %v, want [/app]", navs)
	}
	signedIn := false
	for _, ev := range f.sink.snapshot() {
		if ev.Kind == "login.succeeded" && ev.UserID == "u-alice" && ev.Meta["via"] == "passkey" {
			signedIn = true
		}
	}
	if !signedIn {
		t.Errorf("no passkey login.succeeded for u-alice; events %+v", f.sink.snapshot())
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/?as=alice"),
		wait(`!!window.__gofastr.loadedModules.passkey && !document.getElementById('del').hidden`),
		chromedp.Click(`#del`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
		chromedp.Evaluate(`window.toasts`, &toasts),
	); err != nil {
		t.Fatalf("delete: %v (toasts %v)", err, toasts)
	}
	if creds, _ := f.pk.store.ListPasskeys(context.Background(), "u-alice"); len(creds) != 0 {
		t.Errorf("after delete: %d passkeys, want 0", len(creds))
	}
}

func passkeyBrowser(t *testing.T) context.Context {
	t.Helper()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WSURLReadTimeout(90*time.Second), chromedp.NoSandbox)...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancelTimeout)
	return ctx
}
