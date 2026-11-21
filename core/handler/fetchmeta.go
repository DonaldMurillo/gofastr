package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// IsForgeableRequest reports whether r's Content-Type is a CORS-simple
// one or absent, so a cross-site page could send it without a preflight.
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

// IsCrossSiteRequest reports whether r came from another origin: by
// Sec-Fetch-Site when it is cross-site, same-origin or none, otherwise by
// the Origin host against r.Host. Under same-site an absent, null or
// hostless Origin is cross-site; with no Fetch Metadata it is not.
func IsCrossSiteRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site":
		return true
	case "same-origin", "none":
		return false
	}
	sameSite := r.Header.Get("Sec-Fetch-Site") == "same-site"
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return sameSite
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return sameSite
	}
	return !strings.EqualFold(u.Host, r.Host)
}

// IsCrossSiteRequestStrict is IsCrossSiteRequest that also refuses a bare
// Origin: null with no Sec-Fetch-Site, for fetch-only endpoints.
func IsCrossSiteRequestStrict(r *http.Request) bool {
	if IsCrossSiteRequest(r) {
		return true
	}
	return r.Header.Get("Origin") == "null" && r.Header.Get("Sec-Fetch-Site") == ""
}

// IsForgedCrossSiteWrite reports a method other than GET, HEAD and OPTIONS
// that IsForgeableRequest and IsCrossSiteRequest both report.
func IsForgedCrossSiteWrite(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
		IsForgeableRequest(r) && IsCrossSiteRequest(r)
}

// RefuseCrossSite answers a forged cross-site write 403 before h runs.
func RefuseCrossSite(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsForgedCrossSiteWrite(r) {
			WriteError(w, r, Fail(http.StatusForbidden, ReasonCrossSiteRefused))
			return
		}
		h.ServeHTTP(w, r)
	})
}

// IsCrossOriginSubresource reports a request whose Sec-Fetch-Site is
// present and neither same-origin nor none, unless it is a GET or HEAD
// navigation into a page or frame: a script, image, style, fetch, form
// post or object load from another origin. A same-site sibling counts as
// another origin. A request with no Fetch Metadata is not one.
func IsCrossOriginSubresource(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return false
	}
	if r.Header.Get("Sec-Fetch-Mode") != "navigate" {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "object", "embed":
		return true
	}
	return false
}

// IsolateResources returns middleware that answers 403 to every request
// IsCrossOriginSubresource reports, unless allow (when non-nil) admits it.
func IsolateResources(allow func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Sec-Fetch-Site")
			w.Header().Add("Vary", "Sec-Fetch-Mode")
			w.Header().Add("Vary", "Sec-Fetch-Dest")
			if IsCrossOriginSubresource(r) && (allow == nil || !allow(r)) {
				WriteError(w, r, Fail(http.StatusForbidden, ReasonCrossSiteRefused))
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

// SameSiteHopParam is the query parameter SameSiteHopTarget adds to the
// URL a hop page loads again, so NeedsSameSiteHop does not answer that
// reload with another hop.
const SameSiteHopParam = "gofastr_hop"

// NeedsSameSiteHop reports whether a top-level GET should first get a
// same-origin page that loads it again: it is cross-site, or carries no
// Sec-Fetch-Site and no SameSiteHopParam.
func NeedsSameSiteHop(r *http.Request) bool {
	if IsCrossSiteRequest(r) {
		return true
	}
	return r.Header.Get("Sec-Fetch-Site") == "" && !r.URL.Query().Has(SameSiteHopParam)
}

// SameSiteHopTarget returns target with SameSiteHopParam added when r
// carried no Sec-Fetch-Site and target lacks it. The marker is no
// credential.
func SameSiteHopTarget(r *http.Request, target string) string {
	if r.Header.Get("Sec-Fetch-Site") != "" {
		return target
	}
	path, query, _ := strings.Cut(target, "?")
	if values, err := url.ParseQuery(query); err == nil && values.Has(SameSiteHopParam) {
		return target
	}
	if query == "" {
		return path + "?" + SameSiteHopParam + "=1"
	}
	return target + "&" + SameSiteHopParam + "=1"
}

type sessionRefusalKey struct{}

// TrackSessionRefusal returns a context NoteSessionRefusal can mark,
// and a func that reads the mark. battery/auth's SessionMiddleware uses it.
func TrackSessionRefusal(ctx context.Context) (context.Context, func() bool) {
	var noted atomic.Bool
	return context.WithValue(ctx, sessionRefusalKey{}, &noted), noted.Load
}

// NoteSessionRefusal says the redirect a gate is about to write sends
// the request away for want of a session. A 401 needs no note.
func NoteSessionRefusal(ctx context.Context) {
	if noted, ok := ctx.Value(sessionRefusalKey{}).(*atomic.Bool); ok {
		noted.Store(true)
	}
}
