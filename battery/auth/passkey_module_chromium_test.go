//go:build chromium

package auth

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
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
window.accepted = []; window.creates = 0; window.condGets = 0; window.submitted = [];
const json = (v) => new Response(JSON.stringify(v), {status: 200, headers: {'Content-Type': 'application/json'}});
const later = (ms, v) => new Promise((r) => setTimeout(() => r(v), ms));
window.listIds = [];
const pk = {challenge: 'AAAA', rpId: 'localhost', allowCredentials: [], userVerification: 'required', timeout: 60000};
window.__gofastr = {
  navigate: (p) => window.navs.push(p), invalidate: () => {},
  _toastOrFallback: (c) => window.toasts.push(c.title),
  fetch: (u, init) => {
    const url = new URL(u, location.href), path = url.pathname;
    window.calls.push(path);
    window.bodies.push(path + url.search + ' ' + ((init && init.body) || ''));
    if (path.endsWith('/passkey/list')) {
      const ids = window.listIds.slice();
      return later(window.listDelay || 0, json({rpId: 'localhost', userId: 'AQID', userName: 'a@x', passkeys: ids.map((id) => ({id}))}));
    }
    if (path.endsWith('/login/options') && window.optionsFail)
      return Promise.resolve(new Response(JSON.stringify({error: 'down', reason: 'x'}), {status: 503, headers: {'Content-Type': 'application/json'}}));
    if (path.endsWith('/login/options') && window.slowOptions)
      return later(window.slowOptions, json({publicKey: pk}));
    if (path.endsWith('/register/options') && window.needStepUp && !String(init.body).includes('"proof"'))
      return Promise.resolve(new Response(JSON.stringify({error: 'confirm', reason: 'passkey_reauth_required'}), {status: 403, headers: {'Content-Type': 'application/json'}}));
    if (path.endsWith('/register/options')) return Promise.resolve(json({binding: 'B', publicKey: {challenge: 'AAAA', rp: {id: 'localhost', name: 'x'},
      user: {id: 'AQID', name: 'a@x', displayName: 'a@x'}, pubKeyCredParams: [{type: 'public-key', alg: -7}], excludeCredentials: []}}));
    if (path.endsWith('/options')) return Promise.resolve(json({publicKey: Object.assign({}, pk, {timeout: window.optionsTimeout || 60000})}));
    if (path.endsWith('/passkey/register')) {
      window.listIds = ['AQID'];
      if (window.slowRegister) return later(window.slowRegister, json({}));
    }
    if (path.endsWith('/passkey/login')) {
      if (window.slowLogin) return later(window.slowLogin, json({redirect: '/app'}));
      if (window.loginReason) return Promise.resolve(new Response(JSON.stringify({error: 'passkey sign-in failed', reason: window.loginReason}), {status: 404, headers: {'Content-Type': 'application/json'}}));
      return Promise.resolve(json({redirect: '/app'}));
    }
    if (path.endsWith('/passkey/reauth')) return Promise.resolve(json({proof: 'p'}));
    return Promise.resolve(json({}));
  },
};
PublicKeyCredential.getClientCapabilities = () => Promise.resolve(window.caps || {conditionalGet: true, conditionalCreate: true});
PublicKeyCredential.signalUnknownCredential = (o) => { window.signals.push(o.credentialId); return Promise.resolve(); };
PublicKeyCredential.signalAllAcceptedCredentials = (o) => { window.accepted.push(o.allAcceptedCredentialIds.length); return Promise.resolve(); };
const fake = {toJSON: () => ({id: 'AQID', response: {clientDataJSON: 'AQID', authenticatorData: 'AQID', signature: 'AQID', userHandle: 'AQID', attestationObject: 'AQID', transports: []}})};
navigator.credentials.get = (o) => {
  if (o.mediation === 'conditional') {
    window.condGets++;
    if (window.autoPick) return Promise.resolve(fake);
    return new Promise((_, reject) => o.signal.addEventListener('abort', () => {
      window.aborts++;
      reject(new DOMException('aborted', 'AbortError'));
    }));
  }
  if (window.getError) return Promise.reject(new DOMException('x', window.getError));
  if (window.holdGet) return later(window.holdGet, fake).then((v) => { window.getDone = (window.getDone || 0) + 1; return v; });
  return Promise.resolve(fake);
};
navigator.credentials.create = () => {
  window.creates++;
  if (window.slowCreate) return later(window.slowCreate, fake);
  if (window.holdGet && !window.getDone) window.overlap = true;
  return Promise.resolve(fake);
};
</script>
%s
<script src="/passkey.js"></script><script src="/passkey-manage.js"></script>`

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
	mux.HandleFunc("/passkey-manage.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, passkeyManageJS)
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
		"/guarded": `<script>window.needStepUp = true</script><button id="add" data-passkey-register="/auth" hidden>Add</button>`,
		// The conditional request stays pending: only the button signs in.
		"/unknownauto": `<input data-passkey-autofill="/auth"><button id="in" data-passkey-login="/auth" hidden>Sign in</button>`,
		"/two":         `<input id="a" data-passkey-autofill="/auth"><input id="b" data-passkey-autofill="/auth">`,
		"/late":        `<input id="a" data-passkey-autofill="/auth">`,
		"/gone":        `<script>window.slowOptions = 400</script><input id="who" data-passkey-autofill="/auth">`,
		"/flaky":       `<script>window.optionsFail = true</script><input data-passkey-autofill="/auth">`,
		"/racepick":    `<script>window.autoPick = true; window.slowLogin = 600</script><input data-passkey-autofill="/auth"><button id="in" data-passkey-login="/auth" hidden>Sign in</button>`,
		"/serial": `<script>window.holdGet = 500</script><form onsubmit="event.preventDefault()"><button id="re" data-passkey-reauth="/auth" hidden>Confirm</button></form>
<button id="add" data-passkey-register="/auth" hidden>Add</button>`,
		"/during":    `<script>window.slowCreate = 500; window.slowRegister = 1000; window.listDelay = 800</script><div id="host"></div><button id="add" data-passkey-register="/auth" hidden>Add</button>`,
		"/stale":     `<script>window.listDelay = 500</script><div data-passkey-list="/auth"></div><button id="add" data-passkey-register="/auth" hidden>Add</button>`,
		"/submitter": `<form id="f" onsubmit="event.preventDefault(); window.submitted.push(event.submitter ? event.submitter.name + '=' + event.submitter.value + '@' + event.submitter.formAction : 'form')"><button id="re" name="op" value="close" formaction="/close" data-passkey-reauth="/auth" hidden>Confirm</button></form>`,
		"/nostorage": `<script>Object.defineProperty(window, 'sessionStorage', {get() { throw new Error('blocked'); }})</script><div data-passkey-upgrade="/auth"></div>`,
		"/send":      ``,
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
	if len(bodies) != 2 || bodies[0] != `/auth/passkey/login/options?autofill=1 {}` || bodies[1] != `/auth/passkey/login/options {}` {
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

	// Asked for a step-up, adding a passkey runs one and sends its proof.
	var seq []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/guarded"),
		wait(`!document.getElementById('add').hidden`),
		chromedp.Click(`#add`, chromedp.ByQuery),
		// The add is done once it re-renders the page.
		wait(`window.navs.length === 1`),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.bodies.filter(b => !b.startsWith('/auth/passkey/list')).map(b => b.split(' ')[0] + (b.includes('"proof":"p"') ? '+proof' : '') + (b.includes('"binding":"B"') ? '+binding' : ''))`, &seq),
	); err != nil {
		t.Fatalf("guarded add: %v", err)
	}
	want := []string{"/auth/passkey/register/options", "/auth/passkey/reauth/options", "/auth/passkey/reauth", "/auth/passkey/register/options+proof", "/auth/passkey/register+binding"}
	if !slices.Equal(seq, want) {
		t.Errorf("guarded add requests %v, want %v", seq, want)
	}

	// num evaluates expr on a page once the module has run.
	num := func(page, setup, expr string, acts ...chromedp.Action) int {
		t.Helper()
		var n int
		run := []chromedp.Action{chromedp.Navigate(origin + page), wait(`!!window.__gofastr.loadedModules && window.__gofastr.loadedModules.passkey`)}
		if setup != "" {
			run = append(run, chromedp.Evaluate(setup, nil))
		}
		run = append(run, acts...)
		run = append(run, chromedp.Evaluate(expr, &n))
		if err := chromedp.Run(ctx, run...); err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		return n
	}

	// With the conditional request held pending, only the button's
	// sign-in can report the deleted passkey: it withdraws the request,
	// and its own 404 drops the passkey.
	if n := num("/unknownauto", `window.loginReason = 'passkey_unknown_credential'`, `window.aborts * 10 + window.signals.length`,
		wait(`window.condGets === 1 && !document.getElementById('in').hidden`),
		chromedp.Click(`#in`, chromedp.ByQuery), wait(`window.signals.length === 1`)); n != 11 {
		t.Errorf("button sign-in over a pending autofill: aborts*10+signals = %d, want 11", n)
	}

	// Two autofill fields on one page: one owns the conditional request.
	if n := num("/two", "", `window.calls.filter(c => c.endsWith('/login/options')).length`, chromedp.Sleep(500*time.Millisecond)); n != 1 {
		t.Errorf("two autofill fields asked for %d conditional options, want 1", n)
	}

	// A second field that arrives while the first one's request waits
	// leaves it alone.
	if n := num("/late", "", `window.calls.filter(c => c.endsWith('/login/options')).length * 10 + window.condGets`,
		wait(`window.condGets === 1`),
		chromedp.Evaluate(`const b = document.createElement('input'); b.setAttribute('data-passkey-autofill', '/auth'); document.body.appendChild(b); window.__gofastr._moduleScanners.passkey(document)`, nil),
		chromedp.Sleep(500*time.Millisecond)); n != 11 {
		t.Errorf("a late second autofill field: options*10+gets = %d, want 11", n)
	}

	// The field went away while its options were on their way: no
	// conditional request starts for it.
	if n := num("/gone", `document.getElementById('who').remove()`, `window.condGets`, chromedp.Sleep(800*time.Millisecond)); n != 0 {
		t.Errorf("a removed field's options started %d conditional requests", n)
	}

	// An endpoint failure shows once and is retried later, not at once.
	if n := num("/flaky", "", `window.toasts.length * 100 + window.calls.filter(c => c.endsWith('/login/options')).length`, chromedp.Sleep(1500*time.Millisecond)); n/100 != 1 || n%100 > 2 {
		t.Errorf("autofill against a failing endpoint: %d toasts and %d requests in 1.5s, want 1 and at most 2", n/100, n%100)
	}

	// An autofill pick whose sign-in is still on its way holds the button:
	// one sign-in POST, one navigation.
	if n := num("/racepick", "", `window.calls.filter(c => c === '/auth/passkey/login').length * 10 + window.navs.length`,
		wait(`window.calls.includes('/auth/passkey/login') && !document.getElementById('in').hidden`),
		chromedp.Click(`#in`, chromedp.ByQuery), chromedp.Sleep(1200*time.Millisecond)); n != 11 {
		t.Errorf("button during an autofill sign-in: logins*10+navs = %d, want 11", n)
	}

	// A step-up waiting on the authenticator holds a second ceremony back.
	if n := num("/serial", "", `(window.overlap ? 100 : 0) + window.creates`,
		wait(`!document.getElementById('re').hidden && !document.getElementById('add').hidden`),
		chromedp.Click(`#re`, chromedp.ByQuery), chromedp.Click(`#add`, chromedp.ByQuery),
		wait(`window.creates === 1`)); n != 1 {
		t.Errorf("an add during a pending step-up: overlap/creates = %d, want 1 (no overlap)", n)
	}

	// A list read that began before an add never signals its older list.
	if n := num("/stale", "", `window.accepted.filter(l => l === 0).length * 10 + window.accepted.length`,
		wait(`!document.getElementById('add').hidden`),
		chromedp.Click(`#add`, chromedp.ByQuery), chromedp.Sleep(1500*time.Millisecond)); n != 1 {
		t.Errorf("stale list signals: empty*10+total = %d, want only the fresh one (1)", n)
	}

	// A list read that starts during an add and answers before it ends,
	// with the list from before the add, never signals.
	if n := num("/during", "", `window.accepted.filter(l => l === 0).length * 10 + window.accepted.length`,
		wait(`!document.getElementById('add').hidden`),
		chromedp.Click(`#add`, chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`const l = document.createElement('div'); l.setAttribute('data-passkey-list', '/auth'); document.getElementById('host').appendChild(l); window.__gofastr._moduleScanners['passkey-manage'](document)`, nil),
		wait(`window.navs.length === 1`), chromedp.Sleep(1000*time.Millisecond)); n != 1 {
		t.Errorf("a list read overlapping an add: empty*10+total = %d, want only the fresh one (1)", n)
	}

	// The proof goes as the button would have submitted it.
	var submitted []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/submitter"),
		wait(`!document.getElementById('re').hidden`),
		chromedp.Click(`#re`, chromedp.ByQuery),
		wait(`window.submitted.length === 1`),
		chromedp.Evaluate(`window.submitted`, &submitted),
	); err != nil {
		t.Fatalf("submitter: %v", err)
	}
	if submitted[0] != "op=close@"+origin+"/close" {
		t.Errorf("step-up form submitted as %q, want the button's own name, value and formaction", submitted[0])
	}

	// Without storage to remember the offer, the quiet upgrade never asks.
	if n := num("/nostorage", "", `window.calls.length`, chromedp.Sleep(400*time.Millisecond)); n != 0 {
		t.Errorf("upgrade without storage made %d requests", n)
	}

	// The transport refuses another origin before the host fetch sees it.
	var refused string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(origin+"/send"),
		wait(`!!window.__gofastr._passkey`),
		chromedp.Evaluate(`window.__gofastr._passkey.send('https://evil.example/passkey/list', {}).then(() => 'sent', (e) => e.message + '|' + window.calls.length)`, &refused,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("send: %v", err)
	}
	if refused != "Passkey request failed|0" {
		t.Errorf("off-origin send: %q, want refused with no fetch", refused)
	}
}
