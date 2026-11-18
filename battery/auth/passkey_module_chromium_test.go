//go:build chromium

package auth

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// passkeyStubPage serves passkey.js and a page whose credentials API and
// host fetch are stubs: a conditional get waits until it is aborted, any
// other get answers a fake credential (or throws window.getError), and
// every request is counted in window.calls.
const passkeyStubPage = `<!doctype html><html><meta charset=utf-8>
<script>
window.calls = []; window.toasts = []; window.navs = []; window.aborts = 0; window.bodies = []; window.signals = [];
const json = (v) => new Response(JSON.stringify(v), {status: 200, headers: {'Content-Type': 'application/json'}});
const pk = {challenge: 'AAAA', rpId: 'localhost', allowCredentials: [], userVerification: 'required', timeout: 60000};
window.__gofastr = {
  navigate: (p) => window.navs.push(p), invalidate: () => {},
  _toastOrFallback: (c) => window.toasts.push(c.title),
  fetch: (u, init) => {
    const path = new URL(u, location.href).pathname;
    window.calls.push(path);
    window.bodies.push(path + ' ' + ((init && init.body) || ''));
    if (path.endsWith('/register/options')) return Promise.resolve(json({publicKey: {challenge: 'AAAA', rp: {id: 'localhost', name: 'x'},
      user: {id: 'AQID', name: 'a@x', displayName: 'a@x'}, pubKeyCredParams: [{type: 'public-key', alg: -7}], excludeCredentials: []}}));
    if (path.endsWith('/options')) return Promise.resolve(json({publicKey: Object.assign({}, pk, {timeout: window.optionsTimeout || 60000})}));
    if (path.endsWith('/passkey/login')) {
      if (window.loginReason) return Promise.resolve(new Response(JSON.stringify({error: 'passkey sign-in failed', reason: window.loginReason}), {status: 404, headers: {'Content-Type': 'application/json'}}));
      return Promise.resolve(json({redirect: '/app'}));
    }
    if (path.endsWith('/passkey/reauth')) return Promise.resolve(json({proof: 'p'}));
    return Promise.resolve(json({}));
  },
};
PublicKeyCredential.getClientCapabilities = () => Promise.resolve(window.caps || {conditionalGet: true, conditionalCreate: true});
PublicKeyCredential.signalUnknownCredential = (o) => { window.signals.push(o.credentialId); return Promise.resolve(); };
const fake = {toJSON: () => ({id: 'AQID', response: {clientDataJSON: 'AQID', authenticatorData: 'AQID', signature: 'AQID', userHandle: 'AQID', attestationObject: 'AQID', transports: []}})};
navigator.credentials.get = (o) => {
  if (o.mediation === 'conditional') {
    return new Promise((_, reject) => o.signal.addEventListener('abort', () => {
      window.aborts++;
      reject(new DOMException('aborted', 'AbortError'));
    }));
  }
  if (window.getError) return Promise.reject(new DOMException('x', window.getError));
  return Promise.resolve(fake);
};
navigator.credentials.create = () => Promise.resolve(fake);
</script>
%s
<script src="/passkey.js"></script>`

func TestPasskeyModuleRules(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)
	mux := http.NewServeMux()
	mux.HandleFunc("/passkey.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, passkeyJS)
	})
	pages := map[string]string{
		"/signin":  `<input id="who" data-passkey-autofill="/auth"><button id="in" data-passkey-login="/auth" hidden>Sign in</button>`,
		"/upgrade": `<div data-passkey-upgrade="/auth"></div>`,
		"/reauth":  `<form onsubmit="event.preventDefault()"><button id="re" data-passkey-reauth="/auth" hidden>Confirm</button></form>`,
		// A security-key-only device: no platform authenticator, no hybrid.
		"/add": `<script>window.caps = {passkeyPlatformAuthenticator: false, hybridTransport: false}</script><button id="add" data-passkey-register="/auth" hidden>Add</button>`,
		// A short challenge life, so the autofill request renews within the test.
		"/renew":   `<script>window.optionsTimeout = 2000</script><input data-passkey-autofill="/auth">`,
		"/unknown": `<button id="in" data-passkey-login="/auth" hidden>Sign in</button>`,
	}
	for path, body := range pages {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, passkeyStubPage, body)
		})
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	ctx := chromedptest.Context(t)
	wait := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}

	// A button sign-in withdraws the waiting autofill request, and once it
	// signed in the request is not started again on the page it leaves.
	var options, aborts int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/signin"),
		wait(`window.calls.filter(c => c.endsWith('/login/options')).length === 1 && !document.getElementById('in').hidden`),
		chromedp.Click(`#in`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.calls.filter(c => c.endsWith('/login/options')).length`, &options),
		chromedp.Evaluate(`window.aborts`, &aborts),
	); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if aborts != 1 {
		t.Errorf("the autofill request was withdrawn %d times, want 1", aborts)
	}
	if options != 2 {
		t.Errorf("login/options requested %d times, want 2 (autofill, then the button): autofill restarted after the sign-in", options)
	}

	// The silent create runs once per tab session, not on every load.
	var registers int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/upgrade"),
		wait(`window.calls.includes('/auth/passkey/register')`),
		chromedp.Reload(),
		wait(`!!window.__gofastr.loadedModules && window.__gofastr.loadedModules.passkey`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.calls.filter(c => c.endsWith('/register/options')).length`, &registers),
	); err != nil {
		var calls []string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.calls`, &calls))
		t.Fatalf("upgrade: %v (calls %v)", err, calls)
	}
	if registers != 0 {
		t.Errorf("the reloaded page asked for a conditional create again (%d register/options calls)", registers)
	}

	// InvalidStateError means an excluded credential only for a create;
	// a step-up that hits it shows the browser's own message.
	var toasts []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/reauth"),
		wait(`!document.getElementById('re').hidden`),
		chromedp.Evaluate(`window.getError = 'InvalidStateError'`, nil),
		chromedp.Click(`#re`, chromedp.ByQuery),
		wait(`window.toasts.length === 1`),
		chromedp.Evaluate(`window.toasts`, &toasts),
	); err != nil {
		t.Fatalf("reauth: %v", err)
	}
	if toasts[0] == "This device already holds a passkey for this account" {
		t.Errorf("a step-up's InvalidStateError was reported as an already-registered passkey")
	}

	// Autofill's options say so, and the button's do not.
	var bodies []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/signin"),
		wait(`window.calls.filter(c => c.endsWith('/login/options')).length === 1 && !document.getElementById('in').hidden`),
		chromedp.Click(`#in`, chromedp.ByQuery),
		wait(`window.navs.length === 1`),
		chromedp.Evaluate(`window.bodies.filter(b => b.startsWith('/auth/passkey/login/options'))`, &bodies),
	); err != nil {
		t.Fatalf("options bodies: %v", err)
	}
	if len(bodies) != 2 || bodies[0] != `/auth/passkey/login/options {"autofill":true}` || bodies[1] != `/auth/passkey/login/options {}` {
		t.Errorf("login/options bodies %q, want the autofill flag on the first only", bodies)
	}

	// The waiting autofill request is renewed before its challenge expires.
	var renewals int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/renew"),
		wait(`window.calls.filter(c => c.endsWith('/login/options')).length >= 3`),
		chromedp.Evaluate(`window.calls.filter(c => c.endsWith('/login/options')).length`, &renewals),
	); err != nil {
		t.Fatalf("renew: %v (requests %d)", err, renewals)
	}

	// A device that reports no platform authenticator and no hybrid
	// transport may still hold a security key: the add button shows.
	var addShown bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/add"),
		wait(`!!window.__gofastr.loadedModules && window.__gofastr.loadedModules.passkey`),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`!document.getElementById('add').hidden`, &addShown),
	); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !addShown {
		t.Error("the add button stayed hidden on a device without a platform authenticator or hybrid")
	}

	// Only the unknown-credential reason drops a passkey from the
	// password manager; another 404 leaves it.
	for reason, want := range map[string]int{"passkey_unknown_credential": 1, "not_found": 0} {
		var signals int
		if err := chromedp.Run(ctx,
			chromedp.Navigate(origin+"/unknown"),
			wait(`!document.getElementById('in').hidden`),
			chromedp.Evaluate(`window.loginReason = `+fmt.Sprintf("%q", reason), nil),
			chromedp.Click(`#in`, chromedp.ByQuery),
			wait(`window.toasts.length === 1`),
			chromedp.Sleep(100*time.Millisecond),
			chromedp.Evaluate(`window.signals.length`, &signals),
		); err != nil {
			t.Fatalf("404 %s: %v", reason, err)
		}
		if signals != want {
			t.Errorf("404 with reason %q: %d signalUnknownCredential calls, want %d", reason, signals, want)
		}
	}
}
