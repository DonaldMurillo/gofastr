package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// isFormRequest reports whether the request body is form-encoded (HTML
// form POST). True when Content-Type starts with
// "application/x-www-form-urlencoded" or "multipart/form-data".
func isFormRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}

// decodeAuthCredentials reads either a JSON {email, password} body or an
// HTML form body with the same fields. Returns the populated values and
// a bool indicating whether the request was form-encoded (so the handler
// can pick a form-friendly response shape).
//
// On a body that exceeds the size cap, writes 413 and returns ok=false.
// On any other decode error, writes 400 and returns ok=false.
//
// SECURITY: this intentionally does NOT decode a "roles" field.
// /auth/register is an anonymous endpoint — honoring client-supplied
// roles would let any visitor self-promote to admin via a single POST
// (or a CSRF-style cross-origin form submission). Roles are assigned
// server-side in the handler from a configured default.
func decodeAuthCredentials(w http.ResponseWriter, r *http.Request) (email, password string, form bool, ok bool) {
	if isFormRequest(r) {
		// http.Request.ParseForm respects MaxBytesReader if we set the
		// body wrapper first.
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		if err := r.ParseForm(); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeAuthError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return "", "", true, false
			}
			writeAuthError(w, http.StatusBadRequest, "invalid form body")
			return "", "", true, false
		}
		email = r.PostFormValue("email")
		password = r.PostFormValue("password")
		return email, password, true, true
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return "", "", false, false
	}
	return body.Email, body.Password, false, true
}

// successRedirect returns the redirect target for a form-based auth
// flow. A trusted ?next= override is honored only when it parses as a
// safe same-origin relative path — defending against open-redirect via
//   - protocol-relative ("//evil.example")
//   - absolute URLs ("https://evil.example", "javascript:...")
//   - backslash bypass ("/\evil.example" — browsers normalise \ to /)
//   - control chars / CRLF (header injection)
//
// Anything not matching the safe-relative shape falls back to `fallback`.
func successRedirect(r *http.Request, fallback string) string {
	next := r.URL.Query().Get("next")
	if next == "" {
		if err := r.ParseForm(); err == nil {
			next = r.PostFormValue("next")
		}
	}
	if isSafeRelativePath(next) {
		return next
	}
	return fallback
}

// isSafeRelativePath returns true when p is safe to use directly as a
// Location header for a same-origin redirect. Requires: starts with a
// single '/', no scheme, no host, no backslash, no control bytes, and
// not "//"-prefixed (protocol-relative).
//
// Critically, the check runs against BOTH the raw input (for the
// shape rules) AND the decoded `u.Path` (for percent-encoded
// backslash / control chars / //). Without the second pass, an
// attacker can supply `next=/%5Cevil.example/x` — the raw string
// has no literal '\' so the surface checks pass, then the browser
// decodes %5C to '\' and normalises to '/', landing on
// //evil.example/x cross-origin.
func isSafeRelativePath(p string) bool {
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	if strings.HasPrefix(p, "//") {
		return false
	}
	// Reject dangerous bytes in the raw input.
	if strings.ContainsAny(p, "\\\x00\r\n\t") {
		return false
	}
	// url.Parse catches the remaining shapes (schemes, hosts smuggled
	// via percent-encoded characters, etc.) AND decodes percent-
	// escapes so the post-parse path can be re-checked.
	u, err := url.Parse(p)
	if err != nil {
		return false
	}
	if u.Scheme != "" || u.Host != "" {
		return false
	}
	// Re-check the DECODED path: %5C → \, %00 → NUL, %0d%0a → CRLF.
	// Any of these in the decoded form is just as dangerous as in
	// the raw form — browsers decode before navigating.
	if strings.ContainsAny(u.Path, "\\\x00\r\n\t") {
		return false
	}
	// Protocol-relative after decoding (e.g. `/%2Fevil` decodes to
	// `//evil` if leading slash is one of two).
	if strings.HasPrefix(u.Path, "//") {
		return false
	}
	return true
}

// defaultLoginErrorPath is the operator-configured fallback used by
// writeFormAuthError when no usable same-origin Referer is on the
// request. Empty (the default) preserves the legacy behaviour of
// emitting JSON. Set via SetDefaultLoginErrorPath at startup.
//
// atomic.Pointer wrapping makes concurrent writers (multi-app processes,
// test-parallelism toggles) race-free per the Go memory model. A bare
// string assignment is NOT atomic in Go — torn-string reads can panic
// on bounds-check when the length is updated before the data pointer.
var defaultLoginErrorPath atomic.Pointer[string]

// SetDefaultLoginErrorPath configures the path that form-encoded auth
// errors redirect to when the request has no usable Referer (e.g.
// browser with Referrer-Policy: no-referrer, or a CSRF-stripping
// proxy). Without this, those flows fall through to a JSON 4xx body
// the user sees as a raw blob in their browser. Set to your login
// page (e.g. "/login") so users get bounced back to retry.
//
// Pass "" to disable and restore the JSON-fallback behaviour. Safe
// to call concurrently and from package init.
func SetDefaultLoginErrorPath(path string) {
	if path == "" {
		defaultLoginErrorPath.Store(nil)
		return
	}
	defaultLoginErrorPath.Store(&path)
}

// getDefaultLoginErrorPath is the atomic-pointer read mirror.
func getDefaultLoginErrorPath() string {
	if p := defaultLoginErrorPath.Load(); p != nil {
		return *p
	}
	return ""
}

// writeFormAuthError responds to a form-encoded auth request with a 303
// redirect back to the referring page with an ?error= query string.
// Only same-origin Referers are honored — a cross-origin Referer would
// otherwise turn the auth endpoint into a phishing relay.
//
// When no usable Referer is available, falls back to
// defaultLoginErrorPath (if configured via SetDefaultLoginErrorPath)
// or JSON otherwise.
func writeFormAuthError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	target := safeReferer(r)
	if target == "" {
		fallback := getDefaultLoginErrorPath()
		if fallback != "" && isSafeRelativePath(fallback) {
			target = fallback
		}
	}
	if target == "" {
		writeAuthError(w, status, msg)
		return
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	http.Redirect(w, r, target+sep+"error="+sanitiseErr(msg), http.StatusSeeOther)
}

// safeReferer returns the request's Referer when its host matches the
// request's own Host (same-origin), otherwise "". The Referer's query
// string is dropped — propagating attacker-controlled query params
// from the Referer into the redirect Location is a state-confusion
// vector (the attacker plants `?return=evil` on the referring page,
// the auth-error redirect echoes it, the login page reads `?return=`
// and uses it as the next form action). The error context the caller
// passes via msg is the only query value the redirect carries.
func safeReferer(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	// Relative Referer (rare but possible): keep its path only.
	if u.Host == "" {
		if !isSafeRelativePath(u.Path) {
			return ""
		}
		return u.Path
	}
	// Absolute Referer: must match the request's own Host.
	if !strings.EqualFold(u.Host, r.Host) {
		return ""
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	out := u.Path
	if out == "" {
		out = "/"
	}
	if !isSafeRelativePath(out) {
		return ""
	}
	return out
}

// sanitiseErr makes an error message safe to embed in a URL query value
// AND to read back from server-rendered error pages. Whitespace,
// control characters, URL-special characters, and HTML-special
// characters (<, >, ", ') are all replaced with '+'. Full URL escaping
// would be overkill for the short tags we use ("invalid_credentials"
// etc.); the substitution keeps the message a single round-trippable
// token while neutralising every byte that could break out of the
// surrounding context (CRLF in redirect headers, `<script>` in a flash
// message, `&` in a query string).
func sanitiseErr(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c < 0x20 || c > 0x7e {
			b = append(b, '+')
			continue
		}
		switch c {
		case '&', '=', '#', '?', '%', '+',
			'<', '>', '"', '\'':
			b = append(b, '+')
			continue
		}
		b = append(b, c)
	}
	return string(b)
}
