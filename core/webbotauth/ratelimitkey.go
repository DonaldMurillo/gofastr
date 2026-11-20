package webbotauth

import (
	"net"
	"net/http"

	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// RateLimitKey keys a verified agent by "agent:" plus its directory URL
// and anything else by fallback, or by the remote host with IPv6 cut to
// its /64 when fallback is nil. Run it after the verifier and behind an
// IP-keyed limiter: a signer chooses its URL.
func RateLimitKey(fallback func(*http.Request) string) func(*http.Request) string {
	if fallback == nil {
		fallback = remoteHostKey
	}
	return func(r *http.Request) string {
		if a := AgentFromContext(r.Context()); a != nil && a.URL != "" {
			return "agent:" + a.URL
		}
		return fallback(r)
	}
}

func remoteHostKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return clientip.RateKey(host)
}
