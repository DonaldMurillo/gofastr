package pluginhost

import (
	_ "embed"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// BrokerScriptURL is the platform route serving the generic host broker
// (host/pluginhost.js). It is shared by every heavy-JS plugin — the same script
// is injected once per host page and dispatches to per-plugin adapters.
const BrokerScriptURL = "/__gofastr/plugin/host/pluginhost.js"

//go:embed host/pluginhost.js
var brokerJSBytes []byte

// BrokerRouteMethod is the router method the broker route is registered under.
const BrokerRouteMethod = "GET"

// RegisterBrokerRoute serves the generic host broker at [BrokerScriptURL] on
// the given router. It is IDEMPOTENT: multiple plugins may call it from their
// Init and only the first registration lands (the router would otherwise panic
// on a duplicate pattern). Every plugin that mounts a sandboxed client should
// call this in Init so the host page can load the broker regardless of plugin
// load order.
func RegisterBrokerRoute(rt *router.Router) {
	if routeRegistered(rt, BrokerRouteMethod, BrokerScriptURL) {
		return
	}
	rt.Get(BrokerScriptURL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The broker is a host-page script (full privileges), NOT a framed
		// asset — no CORP/CSP relaxation. It is same-origin and CSP-clean
		// (external <script src>, no inline JS).
		writeAsset(w, r, brokerJSBytes, "text/javascript; charset=utf-8", false)
	}))
}

// routeRegistered reports whether (method, pattern) is already on the router.
// Used to keep [RegisterBrokerRoute] idempotent across plugins.
func routeRegistered(rt *router.Router, method, pattern string) bool {
	for _, rr := range rt.Routes() {
		if rr.Method == method && rr.Pattern == pattern {
			return true
		}
	}
	return false
}

// UIHostOption returns the [uihost.Option] that injects the generic host broker
// into every UIHost-rendered page. Apps using a UIHost pass this to uihost.New.
// A plugin that ships its own adapter should compose this with its adapter
// script (adapter LAST, so the generic broker has defined its registry first):
//
//	uihost.WithExtraScripts(pluginhost.BrokerScriptURL, myPlugin.BrokerScriptURL)
func UIHostOption() uihost.Option {
	return uihost.WithExtraScripts(BrokerScriptURL)
}

// BrokerRegistration documents the JavaScript shape a plugin adapter passes to
// window.__gofastrPluginHost.register(name, registration). It is defined here
// for reference (Worker G, IDE hover, and the platform contract); the generic
// broker consumes it on the client side, not Go.
//
//	window.__gofastrPluginHost.register("wysiwyg", {
//	  manifest: {                              // → [Manifest], serialised to JS
//	    entry:        "/…/editor.html",
//	    isolation:    "sandbox-iframe-opaque",
//	    sandbox:      ["allow-scripts"],
//	    capabilities: ["document:read", …],
//	    minHeight:    "240px",
//	    schema:       "wysiwyg-v1",
//	    title:        "WYSIWYG editor"
//	  },
//	  config: { … },                           // plugin blob bridged in init.config
//	  onEvent: function (method, params, api) {
//	    // api = { request, sendEvent, iframe, marker, form }
//	    // handle plugin-specific events: docChanged, save, requestUpload, …
//	    // and mirror the generic hooks if the e2e depends on plugin-named ones.
//	  }
//	});
//
// The generic broker itself handles the protocol-level events (ready, resize,
// focusChanged, themeApplied, metric, bootError), runs the envelope + source
// check + ready→init handshake, and stashes the generic hooks
// (iframe.__pluginReady / __pluginProbes / __pluginTheme / __pluginLastMetric).
// It calls registration.onEvent for EVERY inbound event after its own handling,
// so an adapter can both handle its own methods and mirror the generic hooks
// under plugin-specific names the tests read.
type BrokerRegistration struct {
	Manifest Manifest                                 `json:"manifest"`
	Config   any                                      `json:"config,omitempty"`
	OnEvent  func(method string, params any, api any) `json:"-"`
}
