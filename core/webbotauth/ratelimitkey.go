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
// it would have had anyway.
//
// The identity is the directory URL, not the key id: a rotated key is the
// same agent, so rotation does not reset the window. The signer chooses
// that URL, though, and anyone with a domain and a key can verify: one
// sender can sign each request under a new path, query or subdomain and
// get a fresh budget each time. This limiter splits a shared egress
// fairly; it does not cap a sender. Keep an IP-keyed limiter in front of
// it, with a ceiling above any single agent's budget, to bound what one
// address can send.
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
