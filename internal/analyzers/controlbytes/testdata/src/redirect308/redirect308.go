// Package redirect308 is framework/uihost's handlePage 308 reduced:
// the request path flows through the redirect resolver into
// http.Redirect's Location (probe
// TestUihostRedRedirect308StripsControlBytes), with the guarded partial
// branch beside it (isSafePartialRedirect is the validator that keeps
// it quiet) and battery/admin's PathValue-into-Location spelling, which
// the oracle found as a sibling of the same class.
package redirect308

import (
	"net/http"
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

// pathValueTarget is battery/admin entitySave's shape: the redirect
// target embeds r.PathValue("id") with no check.
func pathValueTarget(w http.ResponseWriter, r *http.Request) {
	dest := "/admin/e/posts/edit/" + r.PathValue("id")
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
