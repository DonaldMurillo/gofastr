package handler

import (
	"net/http"
	"net/url"
	"strings"
)

// IsForgeableRequest reports whether a cross-site page could have sent
// this request WITHOUT a CORS preflight: the CORS "simple request"
// content types plus the absent header (a bodyless fetch() sends no
// Content-Type). application/json and every other type are not
// forgeable: a cross-site POST carrying one is preflighted.
func IsForgeableRequest(r *http.Request) bool {
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

// IsCrossSiteRequest reports whether the request came from another
// origin, using Fetch Metadata first and the Origin header as the
// fallback. This is the ONE implementation of the repo's cross-site
// form guard; every battery and tool that refuses cross-site POSTs
// calls it and keeps only its own response shape.
//
// Sec-Fetch-Site is authoritative where the browser sends it:
// "same-origin" and "none" (a user-initiated navigation: address bar,
// bookmark) are safe, "cross-site" is refused outright. "same-site" is
// NOT proof of same-origin: the site computation drops the port and
// folds sibling subdomains, so a page on evil.example.com (or on a
// sibling port of a localhost tool) is same-site while its Origin names
// a different origin, and a SameSite cookie still attaches. It falls
// through to the Origin-host comparison, as does any unknown value.
// Two copies of this guard (battery/setup, kiln/chat) trusted
// "same-site" and were driven to an attacker-owned admin account and an
// approved destructive plan by the 2026-09-06 probes; the copies are
// gone.
//
// Without Fetch Metadata, an Origin whose host differs from r.Host is
// cross-site. An absent or opaque ("null") Origin cannot prove an
// attack and is allowed: a legitimate top-level same-origin form
// navigation sends Origin: null too, and non-browser clients (curl,
// tests, native apps) send neither header.
func IsCrossSiteRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site":
		return true
	case "same-origin", "none":
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return !strings.EqualFold(u.Host, r.Host)
}

// IsCrossSiteRequestStrict is IsCrossSiteRequest for surfaces that are
// only ever called by fetch(), never by a form navigation: there an
// opaque "Origin: null" with no Fetch Metadata to vouch for it is the
// sandboxed-iframe / cross-origin-redirect shape, not a legitimate
// top-level navigation, so it is refused too. Where the browser sends
// Sec-Fetch-Site the header decides exactly as in IsCrossSiteRequest.
func IsCrossSiteRequestStrict(r *http.Request) bool {
	if IsCrossSiteRequest(r) {
		return true
	}
	return r.Header.Get("Origin") == "null" && r.Header.Get("Sec-Fetch-Site") == ""
}
