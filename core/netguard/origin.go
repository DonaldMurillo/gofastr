package netguard

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginAllowed reports whether r's Origin header is absent, names r's own
// Host authority, or matches one of allowed exactly (case-insensitively).
func OriginAllowed(r *http.Request, allowed []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, o := range allowed {
		if strings.EqualFold(origin, o) {
			return true
		}
	}
	return false
}
