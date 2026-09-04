package admin

import (
	"net/http"
	"net/url"
	"strings"
)

// rejectCrossSiteForm refuses a browser cross-site submission to a mutating
// admin route and reports whether it wrote a response. Mirrors battery/auth's
// Sec-Fetch-Site convention (core.go rejectCrossSiteForm), which the admin
// battery needs because the CSRF middleware is an optional app-level add-on:
// under the battery's own default mounting (RegisterRoutes = SecurityHeaders +
// gate) a rendered-but-empty hidden _csrf input is the only token the screens
// can carry, so the battery itself must refuse the forgeable cross-site shape.
//
// The gate is isForgeableRequest, NOT the Content-Type alone: a form with
// enctype="text/plain" and a bodyless fetch() POST are CORS-simple, preflight-
// free CSRF vehicles exactly like a urlencoded form, and the queue-replay
// route mutates state with no body at all.
//
// Sec-Fetch-Site is the authoritative signal and is checked FIRST. A form on
// a sibling subdomain (evil.example.com → app.example.com) is "same-site" —
// the SameSite session cookie still attaches — so "same-site" is NOT
// sufficient and falls through to the Origin-host comparison. Non-browser
// clients (curl, tests, native apps) send neither header and pass; an absent
// or opaque ("null") Origin can't prove an attack and is allowed, matching a
// same-origin top-level form navigation.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !isForgeableRequest(r) {
		return false
	}
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
		switch sfs {
		case "cross-site":
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return true
		case "same-origin", "none":
			return false
		}
		// "same-site" and unknown values: fall through to Origin.
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if u, err := url.Parse(o); err == nil && u.Host != "" && !strings.EqualFold(u.Host, r.Host) {
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return true
		}
	}
	return false
}

// isForgeableRequest reports whether a cross-site page could have sent this
// request WITHOUT a CORS preflight (the CORS "simple request" content types
// plus the absent header — a bodyless fetch() sends no Content-Type).
// application/json and every other type are NOT forgeable: a cross-site POST
// carrying one is preflighted, and the framework answers no preflight for
// admin routes.
func isForgeableRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "", "application/x-www-form-urlencoded", "multipart/form-data", "text/plain":
		return true
	}
	return false
}
