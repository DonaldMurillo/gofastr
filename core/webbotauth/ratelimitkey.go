package webbotauth

import (
	"net"
	"net/http"
)

// RateLimitKey returns a key function for a per-key rate limiter (such as
// framework/ratelimit's MiddlewareByKey): a verified Web Bot Auth request
// is keyed by its agent, "agent:" plus the resolved key-directory URL, and
// everything else by fallback(r). A nil fallback keys on the connection's
// remote host.
//
// The point is honest quotas. Keyed by IP, a verified agent behind a busy
// egress shares a budget with everyone on that address, and a spoofed
// User-Agent gets whatever the IP gets. Keyed by identity, the agent's
// budget is its own, and an unverified caller falls back to the IP budget
// it would have had anyway — verification never widens what a stranger
// can do.
//
// The identity is the directory URL, not the key id: a rotated key is the
// same agent, so rotation does not reset the window.
//
// Ordering matters: the verifier middleware must run before the limiter,
// or every request is unverified at keying time. In the framework the
// verifier is installed by WithWebBotAuth before any middleware the app
// adds with Use, so a limiter added later sees the agent.
func RateLimitKey(fallback func(*http.Request) string) func(*http.Request) string {
	if fallback == nil {
		fallback = remoteHost
	}
	return func(r *http.Request) string {
		if a := AgentFromContext(r.Context()); a != nil && a.URL != "" {
			return "agent:" + a.URL
		}
		return fallback(r)
	}
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
