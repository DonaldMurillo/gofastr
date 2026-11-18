package pluginhost

import (
	_ "embed"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// BrokerScriptURL is the route serving the host broker, host/pluginhost.js.
const BrokerScriptURL = "/__gofastr/plugin/host/pluginhost.js"

//go:embed host/pluginhost.js
var brokerJSBytes []byte

// BrokerRouteMethod is the broker route's method.
const BrokerRouteMethod = "GET"

// RegisterBrokerRoute serves the host broker at [BrokerScriptURL]; idempotent.
func RegisterBrokerRoute(rt *router.Router) {
	if routeRegistered(rt, BrokerRouteMethod, BrokerScriptURL) {
		return
	}
	rt.Get(BrokerScriptURL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAsset(w, r, brokerJSBytes, "text/javascript; charset=utf-8", responsePolicy{})
	}))
}

func routeRegistered(rt *router.Router, method, pattern string) bool {
	for _, rr := range rt.Routes() {
		if rr.Method == method && rr.Pattern == pattern {
			return true
		}
	}
	return false
}

// UIHostOption injects the host broker into every UIHost page.
func UIHostOption() uihost.Option {
	return uihost.WithExtraScripts(BrokerScriptURL)
}

// BrokerRegistration is the shape a JavaScript adapter registers with.
type BrokerRegistration struct {
	Manifest Manifest                                 `json:"manifest"`
	Config   any                                      `json:"config,omitempty"`
	OnEvent  func(method string, params any, api any) `json:"-"`
}
