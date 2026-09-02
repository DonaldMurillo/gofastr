// Package outboundhdr pins the outbound-request header posture (review
// finding C6): http.Header.Set/Add on a map whose provenance is an
// OUTBOUND *http.Request — an http.NewRequest/NewRequestWithContext
// result, or any request-typed value that is not the inbound handler
// parameter — is silent, because the client transport rejects control
// bytes at write time (net/http: invalid header field value for ...).
// The response writer's header map, directly or through a local, and
// the inbound parameter's map, still fire: writing those is the
// terminal sink this rule exists for.
package outboundhdr

import (
	"net/http"
)

// forward is the reverse-proxy/API-gateway idiom: inbound headers
// copied onto an outbound request. Quiet by decision C6.
func forward(r *http.Request) error {
	out, err := http.NewRequest(http.MethodGet, "https://upstream.example", nil)
	if err != nil {
		return err
	}
	out.Header.Set("X-Request-Id", r.Header.Get("X-Request-Id"))
	out.Header.Set("X-Forwarded-For", r.RemoteAddr)
	_, err = http.DefaultClient.Do(out)
	return err
}

// forwardedLocal is the same silence through a local header map and a
// NewRequestWithContext provenance.
func forwardedLocal(r *http.Request) error {
	out, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://upstream.example", nil)
	hdr := out.Header
	hdr.Add("X-Request-Id", r.Header.Get("X-Request-Id"))
	_, err := http.DefaultClient.Do(out)
	return err
}

// responseStillFires: w.Header() directly and through a local.
func responseStillFires(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Debug-Path", r.URL.Path) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
	h := w.Header()
	h.Add("X-Debug-Host", r.Host) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// inboundParamStillFires: a *http.Request handler parameter is inbound,
// not outbound provenance, so its header map keeps firing.
func inboundParamStillFires(r *http.Request) {
	r.Header.Set("X-Debug-Path", r.URL.Path) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}
