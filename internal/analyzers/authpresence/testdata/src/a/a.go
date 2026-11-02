package a

import (
	"net/http"
	"strings"

	"lib"
)

// preFix2cfff500 is core/middleware SkipBearerAuth before 2cfff500.
func preFix2cfff500() func(*http.Request) bool {
	return func(r *http.Request) bool {
		if r.Header.Get("Authorization") != "" { // want `authpresence: an Authorization header's presence is not its scheme`
			return true
		}
		if r.Header.Get("X-API-Key") != "" {
			return true
		}
		return false
	}
}

// fixed2cfff500 is the fix spelling: parse the scheme.
func fixed2cfff500() func(*http.Request) bool {
	return func(r *http.Request) bool {
		if scheme, _, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " "); ok && strings.EqualFold(scheme, "bearer") {
			return true
		}
		return r.Header.Get("X-API-Key") != ""
	}
}

// boundLocal tests the presence of a local bound to the read.
func boundLocal(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("authorization"))
	return auth == "" // want `authpresence: an Authorization`
}

// lenPresence measures instead of comparing.
func lenPresence(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	return len(h) > 0 // want `authpresence: an Authorization`
}

// extractBearer is battery/auth's extractBearerToken: an empty check
// guarding a parse of the same value.
func extractBearer(r *http.Request) string { // want extractBearer:"parsesAuth"
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}

// anonymousThenParse is a presence check guarding a same-package parse
// that reads the header itself.
func anonymousThenParse(w http.ResponseWriter, r *http.Request, next http.Handler) { // want anonymousThenParse:"parsesAuth"
	if r.Header.Get("Authorization") == "" {
		next.ServeHTTP(w, r)
		return
	}
	_ = extractBearer(r)
}

// crossPackageParse delegates to another package's parser.
func crossPackageParse(r *http.Request) bool { // want crossPackageParse:"parsesAuth"
	if r.Header.Get("Authorization") == "" {
		return false
	}
	_, ok := lib.BearerToken(r)
	return ok
}

// forwards passes the value on: the receiver decides.
func forwards(r, out *http.Request) {
	if h := r.Header.Get("Authorization"); h != "" {
		out.Header.Set("Authorization", h)
	}
}

// Near miss: a request passed to an unrelated handler is no parse.
func inlineSkip(w http.ResponseWriter, r *http.Request, next http.Handler, check func(*http.Request) bool) {
	if r.Header.Get("Authorization") != "" { // want `authpresence: an Authorization`
		next.ServeHTTP(w, r)
		return
	}
	if !check(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	next.ServeHTTP(w, r)
}

// Near miss: another header's presence is fine.
func apiKeyPresence(r *http.Request) bool {
	return r.Header.Get("X-API-Key") != ""
}

const csrfCookie = "auth_csrf"

// preFix2cfff500Cookie is battery/auth CSRFToken's fallback before
// 2cfff500: the bare name first.
func preFix2cfff500Cookie(r *http.Request) string {
	for _, name := range []string{csrfCookie, "__Host-" + csrfCookie} { // want `authpresence: this cookie fallback reads csrfCookie before "__Host-" \+ csrfCookie`
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// fixed2cfff500Cookie lists the prefixed name first.
func fixed2cfff500Cookie(r *http.Request) string {
	for _, name := range []string{"__Host-" + csrfCookie, csrfCookie} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// literalPair spells both names as constants.
func literalPair(r *http.Request) string {
	for _, name := range []string{"session", "__Secure-session"} { // want `authpresence: this cookie fallback reads "session" before "__Secure-session"`
		if c, err := r.Cookie(name); err == nil {
			return c.Value
		}
	}
	return ""
}

// clearsBoth only writes cookies: the order is irrelevant.
func clearsBoth(w http.ResponseWriter) {
	for _, name := range []string{csrfCookie, "__Host-" + csrfCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, MaxAge: -1})
	}
}

// Near miss: unrelated names that merely share a suffix.
func unrelated(r *http.Request) string {
	for _, name := range []string{"a_session", "__Host-session"} {
		if c, err := r.Cookie(name); err == nil {
			return c.Value
		}
	}
	return ""
}
