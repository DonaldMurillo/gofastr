//go:build chromium

package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The module's management and step-up paths in a real browser: the
// transports Chrome reports reach the store, a rename and a step-up
// round-trip, a deleted passkey answers 404 and the module tells the
// password manager to drop it, and the autofill field gains the
// webauthn token without blocking a button sign-in.
func TestPasskeyBrowserManageAndReauth(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	origin := fmt.Sprintf("http://localhost:%d", port)
	f := newPasskeyFixtureFor(t, "localhost", origin)

	var mu sync.Mutex
	var redeemed []error
	mux := http.NewServeMux()
	mux.Handle("/auth/", f.r)
	mux.HandleFunc("/passkey.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, passkeyJS)
	})
	mux.HandleFunc("/passkey-manage.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, passkeyManageJS)
	})
	mux.HandleFunc("POST /sensitive", func(w http.ResponseWriter, r *http.Request) {
		err := f.pk.RedeemReauthProof(r, r.FormValue(PasskeyReauthField))
		mu.Lock()
		redeemed = append(redeemed, err)
		mu.Unlock()
		fmt.Fprint(w, "<!doctype html><p id=done>done</p>")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("as") == "alice" {
			mintSessionCookie(w, f.mgr.Config(), f.session(t, "u-alice"), time.Now().Add(time.Hour))
		}
		// The autofill field is a signed-out page's: Chrome's virtual
		// authenticator answers its request at once.
		autofill := ""
		if r.URL.Query().Get("autofill") == "1" {
			autofill = `<input id="who" autocomplete="username" data-passkey-autofill="/auth">`
		}
		id := ""
		if creds, _ := f.pk.store.ListPasskeys(r.Context(), "u-alice"); len(creds) > 0 {
			id = PasskeyID(creds[0].ID)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Navigate, toast and the Signal API are stubbed so the test
		// reads what the module asked of them.
		fmt.Fprintf(w, `<!doctype html><html><meta charset=utf-8><meta name="csrf-token" content="%[1]s">
<script>window.navs = []; window.toasts = []; window.signals = [];
window.__gofastr = {navigate: function (p) { window.navs.push(p); }, invalidate: function () {},
  _toastOrFallback: function (c) { window.toasts.push(c.title); }};
for (const k of ['signalUnknownCredential', 'signalAllAcceptedCredentials', 'signalCurrentUserDetails']) {
  PublicKeyCredential[k] = function (o) { window.signals.push([k, o]); return Promise.resolve(); };
}</script>
<form><input data-passkey-name value="Laptop">
<button id="add" data-passkey-register="/auth" hidden>Add</button>
<button id="ren" data-passkey-rename="/auth" data-passkey-id="%[2]s" hidden>Rename</button></form>
<form method="post" action="/sensitive"><input type="hidden" name="_csrf" value="%[1]s"><input type="hidden" name="requestSubmit" value="field">
<button id="re" data-passkey-reauth="/auth" hidden>Confirm</button></form>
%[3]s
<button id="in" data-passkey-login="/auth" hidden>Sign in</button>
<div data-passkey-list="/auth"></div>
<script src="/passkey.js"></script><script src="/passkey-manage.js"></script>`, CSRFToken(r), id, autofill)
	})
	srv := &http.Server{Handler: CSRF()(mux)}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	ctx := chromedptest.Context(t)
	enableVirtualAuthenticator(t, ctx)
	wait := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var toasts []string
	fail := func(step string, err error) {
		t.Helper()
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.toasts`, &toasts))
		t.Fatalf("%s: %v (toasts %v)", step, err, toasts)
	}

	// Add, and the store keeps the transport Chrome reported.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/?as=alice"),
		wait(`!!window.__gofastr.loadedModules.passkey && !document.getElementById('add').hidden`),
		chromedp.Click(`#add`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
	); err != nil {
		fail("register", err)
	}
	creds, _ := f.pk.store.ListPasskeys(context.Background(), "u-alice")
	if len(creds) != 1 || !slices.Contains(creds[0].Transports, "internal") {
		t.Fatalf("after register: %+v; want one passkey with transport internal", creds)
	}

	// A second add from the same authenticator matches excludeCredentials;
	// the browser's InvalidStateError gets its own message.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/"),
		wait(`!!window.__gofastr.loadedModules.passkey && !document.getElementById('add').hidden`),
		chromedp.Click(`#add`, chromedp.ByQuery),
		wait(`window.toasts.length === 1`),
		chromedp.Evaluate(`window.toasts`, &toasts),
	); err != nil {
		fail("second register", err)
	}
	if len(toasts) != 1 || toasts[0] != "This device already holds a passkey for this account" {
		t.Errorf("second add toasts %v, want the already-registered message", toasts)
	}

	// The step-up below uses the passkey this test registered moments
	// earlier: from here only the predate rule applies, so it signs in
	// again first. (With the age rule on, the second add above ran before
	// the account's gate applied and met the browser's own refusal.)
	f.pk.config.ReauthMinAge = 0

	// Rename, then step up: the form posts a proof the handler spends.
	var listSignals int
	if err := chromedp.Run(ctx,
		// Stores keep whole seconds: the new session must start in a
		// later second than the passkey for the passkey to predate it.
		chromedp.Sleep(1100*time.Millisecond),
		chromedp.Navigate(origin+"/?as=alice"),
		wait(`!!window.__gofastr.loadedModules.passkey && !document.getElementById('ren').hidden`),
		// The page's list told the password manager what is current.
		wait(`window.signals.some(s => s[0] === 'signalAllAcceptedCredentials')`),
		chromedp.Evaluate(`window.signals.filter(s => s[0] === 'signalAllAcceptedCredentials' && s[1].allAcceptedCredentialIds.length === 1).length`, &listSignals),
		chromedp.SetValue(`[data-passkey-name]`, "Work laptop", chromedp.ByQuery),
		chromedp.Click(`#ren`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
		chromedp.Click(`#re`, chromedp.ByQuery),
		chromedp.WaitReady(`#done`, chromedp.ByQuery),
	); err != nil {
		fail("rename and reauth", err)
	}
	if listSignals == 0 {
		t.Error("the list did not signal the account's accepted passkeys")
	}
	if c, _ := f.pk.store.GetPasskey(context.Background(), creds[0].ID); c.Name != "Work laptop" {
		t.Errorf("after rename: %q", c.Name)
	}
	mu.Lock()
	if len(redeemed) != 1 || redeemed[0] != nil {
		t.Errorf("the step-up form's proof: %v, want one accepted", redeemed)
	}
	mu.Unlock()

	// Removed on the server, still on the device: a sign-in answers 404
	// and the module tells the password manager to drop it. The autofill
	// field's own request waits meanwhile and must not block the button.
	if _, err := f.pk.store.DeletePasskey(context.Background(), "u-alice", creds[0].ID); err != nil {
		t.Fatal(err)
	}
	var ac string
	var unknown []any
	if err := chromedp.Run(ctx,
		network.ClearBrowserCookies(),
		chromedp.Navigate(origin+"/?autofill=1"),
		wait(`!!window.__gofastr.loadedModules.passkey && !document.getElementById('in').hidden`),
		wait(`document.getElementById('who').autocomplete.includes('webauthn')`),
		chromedp.AttributeValue(`#who`, "autocomplete", &ac, nil, chromedp.ByQuery),
		chromedp.Click(`#in`, chromedp.ByQuery),
		wait(`window.signals.some(s => s[0] === 'signalUnknownCredential')`),
		chromedp.Evaluate(`window.signals.filter(s => s[0] === 'signalUnknownCredential').map(s => s[1].credentialId)`, &unknown),
	); err != nil {
		fail("unknown credential", err)
	}
	if ac != "username webauthn" {
		t.Errorf("autofill field autocomplete = %q, want %q", ac, "username webauthn")
	}
	// Chrome's virtual authenticator answers the autofill request too, so
	// it may report the same passkey more than once; every report must
	// name it.
	if len(unknown) == 0 || slices.ContainsFunc(unknown, func(id any) bool { return id != PasskeyID(creds[0].ID) }) {
		t.Errorf("signalUnknownCredential ids %v, want only %s", unknown, PasskeyID(creds[0].ID))
	}
}
