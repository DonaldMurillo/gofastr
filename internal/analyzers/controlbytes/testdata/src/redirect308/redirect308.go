// Package redirect308 is framework/uihost's handlePage 308 reduced:
// the request path flows through the redirect resolver into
// http.Redirect's Location (probe
// TestUihostRedRedirect308StripsControlBytes), with the guarded partial
// branch beside it (isSafePartialRedirect is the validator that keeps
// it quiet) and battery/admin's PathValue-into-Location spelling, which
// the oracle found as a sibling of the same class.
package redirect308

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

func resolve(app *app, path string) (string, bool) {
	return app.resolveRedirect(path)
}

type app struct{ to string }

func (a *app) resolveRedirect(path string) (string, bool) {
	return a.to + path, true
}

// handlePage is the pre-fix 308: the substituted target carries the
// percent-decoded request path straight into Location.
func handlePage(w http.ResponseWriter, r *http.Request, a *app) {
	path := r.URL.Path
	if target, ok := resolve(a, path); ok {
		http.Redirect(w, r, target, http.StatusPermanentRedirect) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
		return
	}
}

// handlePartial is the guarded branch of the same value: the validator
// vetted it, so both the header and a redirect after the guard are
// quiet.
func handlePartial(w http.ResponseWriter, r *http.Request, a *app) {
	path := r.URL.Path
	if target, ok := resolve(a, path); ok && isSafePartialRedirect(target) {
		w.Header().Set("X-Gofastr-Location", target)
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
}

// ignoredVerdict is ledger I9 of the 2026-10-27 bug bash: the validator
// ran, but its answer was thrown away, so it vetted nothing.
func ignoredVerdict(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path
	_ = isSafePartialRedirect(target)
	isSafePartialRedirect(target)
	http.Redirect(w, r, target, http.StatusSeeOther) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
}

// keptVerdict stores the answer and gates on it: quiet.
func keptVerdict(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path
	ok := isSafePartialRedirect(target)
	if !ok {
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// mustBeSafe has no result: a call to it aborts on bad input, so the
// statement form still vets.
func mustBeSafe(p string) {
	if !isSafePartialRedirect(p) {
		panic("unsafe redirect")
	}
}

func voidValidator(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path
	mustBeSafe(target)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// pathValueTarget is a record-save handler's shape: the redirect
// target embeds r.PathValue("id") with no check.
func pathValueTarget(w http.ResponseWriter, r *http.Request) {
	dest := "/admin/entities/posts/" + r.PathValue("id")
	http.Redirect(w, r, dest+"?e=token", http.StatusSeeOther) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
}

// literalTarget is quiet by construction: nothing request-derived in
// the Location.
func literalTarget(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// trailingSlash is the uihost :3058 sibling: path plus query into
// Location, unvalidated.
func trailingSlash(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
}

// isSafePartialRedirect is framework/uihost safe_path.go's validator:
// no control bytes, no scheme, no host, no protocol-relative form.
func isSafePartialRedirect(p string) bool {
	if p == "" || !hasPrefix(p, "/") || hasPrefix(p, "//") {
		return false
	}
	for i := range len(p) {
		if p[i] == '\\' || p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	return true
}

func hasPrefix(s, pre string) bool {
	return len(s) >= len(pre) && s[:len(pre)] == pre
}

// reassignAfterVerdict: the validator vetted the ORIGINAL value; the
// reassignment replaces it with bytes nobody vetted. Until 2026-11-03
// a guard covered the variable for the rest of the function, so the
// re-read slipped through. Fires.
func reassignAfterVerdict(w http.ResponseWriter, r *http.Request) {
	target := "/fixed"
	if !isSafePartialRedirect(target) {
		return
	}
	target = r.URL.Path
	http.Redirect(w, r, target, http.StatusSeeOther) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
}

// reVettedAfterReassign re-vets after the reassignment: quiet.
func reVettedAfterReassign(w http.ResponseWriter, r *http.Request) {
	target := "/fixed"
	if !isSafePartialRedirect(target) {
		return
	}
	target = r.URL.Path
	if !isSafePartialRedirect(target) {
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// reassignAfterSink reassigns only after the vetted value was used:
// the sink ran on the vetted bytes. Quiet.
func reassignAfterSink(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path
	if !isSafePartialRedirect(target) {
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	target = "/fixed"
	_ = target
}

// conditionalRebind replaces the value only on the invalid branch —
// and with literal bytes; on the other the vetted value reaches the
// sink. A rebind inside a branch does not end the vetting. Quiet
// (core/middleware requestid's spelling).
func conditionalRebind(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Request-Id")
	if !isSafePartialRedirect(id) {
		id = "/fresh"
	}
	w.Header().Set("X-Request-Id", id)
}

// conditionalRawRebind replaces the vetted value on one branch with
// fresh request bytes: on that path the sink sees bytes nobody vetted.
func conditionalRawRebind(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Request-Id")
	if !isSafePartialRedirect(id) {
		return
	}
	if r.URL.Query().Has("alt") {
		id = r.Header.Get("X-Alt-Id")
	}
	w.Header().Set("X-Request-Id", id) // want `request-derived`
}

func conditionalAliasedRebind(r *http.Request, replace bool) {
	id := r.Header.Get("X-Request-ID")
	unchecked := r.Header.Get("X-Other-ID")
	if !validRequestID(id) {
		return
	}
	if replace {
		id = unchecked
	}
	slog.Info("request", "id", id) // want `request-derived`
}

func conditionalAliasedChainRebind(w http.ResponseWriter, r *http.Request, replace bool) {
	id := r.Header.Get("X-Request-ID")
	unchecked := r.Header.Get("X-Other-ID")
	alias := unchecked
	if !validRequestID(id) {
		return
	}
	if replace {
		id = alias
	}
	w.Header().Set("X-Request-ID", id) // want `request-derived`
}

func conditionalScrubbedAlias(w http.ResponseWriter, r *http.Request, replace bool) {
	id := r.Header.Get("X-Request-ID")
	checked := strconv.Quote(r.Header.Get("X-Other-ID"))
	if !validRequestID(id) {
		return
	}
	if replace {
		id = checked
	}
	w.Header().Set("X-Request-ID", id)
}

func validRequestID(value string) bool {
	return !strings.ContainsAny(value, "\r\n\x00\x1b")
}

// conditionalSelfRebind: a branch that rebinds the vetted value from
// itself keeps the bytes the guard vetted.
func conditionalSelfRebind(w http.ResponseWriter, r *http.Request, lower bool) {
	id := r.Header.Get("X-Request-ID")
	if !validRequestID(id) {
		return
	}
	if lower {
		id = strings.ToLower(id)
	}
	w.Header().Set("X-Request-ID", id)
}

// conditionalSelfPlusRaw: mixing the vetted value with fresh request
// bytes is fresh request bytes.
func conditionalSelfPlusRaw(w http.ResponseWriter, r *http.Request, join bool) {
	id := r.Header.Get("X-Request-ID")
	if !validRequestID(id) {
		return
	}
	if join {
		id = id + r.Header.Get("X-Suffix")
	}
	w.Header().Set("X-Request-ID", id) // want `request-derived`
}
