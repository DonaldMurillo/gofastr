package web

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func guardLoopback(w http.ResponseWriter, r *http.Request) bool {
	if !loopbackHost(r.Host) {
		i18n.HTTPError(w, r, http.StatusForbidden, reasonNonLoopbackHost)
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonOriginRefused)
		return true
	}
	return false
}

func loopbackHost(host string) bool {
	h := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}
