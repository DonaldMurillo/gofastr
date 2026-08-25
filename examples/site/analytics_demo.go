package main

// Fake first-party analytics wiring — TEST/DEMO ONLY.
//
// This file exists so the e2e suite (e2e_analytics_test.go) can prove the
// shipped first-party analytics mechanisms end to end against a
// PostHog-shaped fake vendor, with zero vendor accounts:
//
//   - battery/relay mounts the vendor's SDK and event endpoint on THIS
//     origin (default mount /__gofastr/t), so the strict default CSP
//     (default-src 'self') needs no script-src/connect-src exceptions and
//     the page never contacts a third-party origin.
//   - uihost.ScriptHandler + uihost.ScriptURL + RegisterExternalScript
//     serve the host-authored bootstrap as an external, hash-versioned
//     script — zero inline JS anywhere.
//   - handler.GetUser backs a whoami endpoint so the bootstrap can
//     identify the visitor when an auth chain is present.
//
// Inert unless BOTH env vars are set (the e2e suite sets them via
// t.Setenv before booting the app):
//
//	SITE_ANALYTICS_FAKE=1
//	SITE_ANALYTICS_FAKE_UPSTREAM=http://127.0.0.1:<port>
//
// The default site build never sets them, so its behavior is byte-for-byte
// identical with this file present: wireFakeAnalytics returns before
// touching the app. relay.New itself enforces that the upstream is a
// loopback http:// origin — exactly what a test httptest server is.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/relay"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// fakeAnalyticsBootstrapJS is the host-authored page bootstrap, the recipe
// a real integration would ship: configure the SDK, load it through the
// relay, resolve identity, fire the initial pageview, and translate the
// runtime's SPA navigation event into one pageview per destination.
//
// Served by uihost.ScriptHandler as an EXTERNAL script (CSP: no inline JS)
// and registered with RegisterExternalScript so every full-page render
// emits it after runtime.js. SPA partial responses never carry it, so the
// listener survives client-side navigation exactly once.
const fakeAnalyticsBootstrapJS = `(function () {
  'use strict';
  // The relayed mount points (fixed at wiring time; both same-origin).
  // The events URL carries a tail segment ("batch") the way real vendor
  // SDKs do: the relay maps the sanitized tail verbatim onto the upstream
  // base path, and an empty tail would join as "/e" (path.Join drops the
  // trailing slash).
  var SDK_URL = '/__gofastr/t/js/sdk.js';
  var EVENTS_URL = '/__gofastr/t/e/batch';

  // Configure the vendor SDK before it loads: it reads this on execution.
  window.__fakeVendorCfg = { eventsURL: EVENTS_URL };

  function vendor() { return window.__fakeVendor; }
  function pageview(path) {
    var v = vendor();
    if (v && v.capture) v.capture('$pageview', { path: path });
  }

  // SPA navigation: the runtime swaps <main> without a reload and fires
  // gofastr:navigate carrying the destination path.
  window.addEventListener('gofastr:navigate', function (e) {
    pageview((e.detail && e.detail.path) || location.pathname);
  });

  var s = document.createElement('script');
  s.src = SDK_URL;
  s.async = true;
  s.onload = function () {
    // Who is the current visitor? Anonymous until the app says otherwise.
    fetch('/__site/analytics/whoami', { headers: { 'Accept': 'application/json' } })
      .then(function (r) { return r.json(); })
      .then(function (me) {
        var v = vendor();
        if (v && v.identify && me && me.id) v.identify(me.id);
        pageview(location.pathname);
      })
      .catch(function () { /* analytics must never break the page */ });
  };
  document.head.appendChild(s);
})();
`

// wireFakeAnalytics wires the fake analytics integration into the site app
// when the test env gate is on. host is the site's uihost, app the
// wrapping framework.App. See the file comment for the full contract.
func wireFakeAnalytics(host *uihost.UIHost, app *framework.App) {
	if os.Getenv("SITE_ANALYTICS_FAKE") != "1" {
		return // default build: nothing below runs
	}
	upstream := os.Getenv("SITE_ANALYTICS_FAKE_UPSTREAM")
	if upstream == "" {
		panic("analytics_demo: SITE_ANALYTICS_FAKE=1 requires SITE_ANALYTICS_FAKE_UPSTREAM (a loopback http:// origin)")
	}

	// 1. The relay: vendor SDK + event beacons become same-origin. The
	// assets route is CacheOK (an immutable, versioned script); the
	// events route is a POST-only beacon endpoint.
	app.RegisterPlugin(relay.New(relay.Config{
		Routes: []relay.Route{
			{Prefix: "js/", Upstream: upstream + "/js", Methods: []string{http.MethodGet}, CacheOK: true},
			{Prefix: "e/", Upstream: upstream + "/e", Methods: []string{http.MethodPost}},
		},
	}))

	// 2. The bootstrap: host-authored external script, served with the
	// framework's versioned-script caching policy (strong ETag, immutable
	// on matching ?v=) and registered onto every full-page render.
	js := []byte(fakeAnalyticsBootstrapJS)
	app.Router().Get("/__site/analytics/bootstrap.js", uihost.ScriptHandler(js))
	if err := host.RegisterExternalScript(uihost.ScriptURL("/__site/analytics/bootstrap.js", js)); err != nil {
		panic("analytics_demo: RegisterExternalScript: " + err.Error())
	}

	// 3. Identity endpoint: who is the current visitor? Anonymous (id
	// null) unless an auth chain put a user in the request context.
	app.Router().Get("/__site/analytics/whoami", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		var id any // nil marshals to JSON null
		if u, ok := handler.GetUser(r.Context()); ok && u != nil {
			switch v := u.(type) {
			case string:
				id = v
			case fmt.Stringer:
				id = v.String()
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	}))

	// The e2e harness drives app.Router() directly (no App.Start), so run
	// the plugin Inits explicitly — the same call Start makes; it is
	// idempotent and documents the production flow.
	if err := app.InitPlugins(); err != nil {
		panic("analytics_demo: InitPlugins: " + err.Error())
	}
}
