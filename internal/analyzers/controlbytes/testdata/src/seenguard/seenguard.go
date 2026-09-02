// Package seenguard pins the membership-guard posture (review finding
// C8): a map membership counts as an allowlist only when the lookup
// keys a value the code has already vetted — the index appears in the
// condition of an if statement or switch case that lexically encloses
// the sink, so the sink runs only for values that ARE members of the
// configured set. A dedup/seen map gates nothing about the bytes: the
// sink runs exactly when the value was never vetted.
package seenguard

import (
	"net/http"
)

var seen = map[string]bool{}
var allowed = map[string]bool{}

// dedupEcho: seen[p] is a dedup guard, not an allowlist — the sink
// after the closed if runs only for FIRST-SEEN values, which no vetting
// ever covered. Fires.
func dedupEcho(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if seen[p] {
		return
	}
	seen[p] = true
	w.Header().Set("X-Debug-Path", p) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// allowEcho: allowed[p] in the enclosing if's condition — the value
// reached the sink only by being a member of the configured set. Quiet.
func allowEcho(w http.ResponseWriter, r *http.Request) {
	p := r.Header.Get("Origin")
	if allowed[p] {
		w.Header().Set("Access-Control-Allow-Origin", p)
	}
}

// afterIf: membership tested in an if that CLOSES before the sink —
// the vetting does not cover the later use. Fires.
func afterIf(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if allowed[p] {
		_ = p
	}
	w.Header().Set("X-Debug-Path", p) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// switchAllow: membership in a switch case's expression gates only that
// case's body. The default arm is not covered. Fires there, quiet in
// the case.
func switchAllow(w http.ResponseWriter, r *http.Request) {
	p := r.Header.Get("Origin")
	switch {
	case allowed[p]:
		w.Header().Set("Access-Control-Allow-Origin", p)
	default:
		w.Header().Set("X-Other", p) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
	}
}

// negatedCommaOkAllow is the repo's BFF spelling: membership tested
// negated, the denial arm diverges before the sink. Everything after
// the if runs only for members of the configured set, so it is the
// same vetting the enclosing-if form gets. Quiet.
func negatedCommaOkAllow(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if _, ok := allowed[origin]; !ok {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
}

// negatedDirectAllow: the same denial without comma-ok. Quiet.
func negatedDirectAllow(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !allowed[origin] {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
}

// negatedNoDenial: negated membership whose arm does NOT diverge gates
// nothing after it. Fires.
func negatedNoDenial(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !allowed[origin] {
		_ = origin
	}
	w.Header().Set("Access-Control-Allow-Origin", origin) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}
