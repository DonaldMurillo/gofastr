// Package ratelimit mirrors framework/ratelimit around be0acf07.
package ratelimit

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"time"
)

type Limiter struct{ trustXFF bool }

func (rl *Limiter) Allow(key string) (bool, time.Duration) { return true, 0 }

func (rl *Limiter) AllowContext(ctx context.Context, key string) (bool, time.Duration) {
	return true, 0
}

func clientIPFor(r *http.Request, trustXFF bool) string { // want clientIPFor:"rawAddr"
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		peer = host
	}
	if !trustXFF {
		return peer
	}
	if ip := net.ParseIP(r.Header.Get("X-Forwarded-For")); ip != nil {
		return ip.String()
	}
	return peer
}

// ClientIP is the full address, for logs: raw on purpose.
func ClientIP(r *http.Request, trustXFF bool) string { // want ClientIP:"rawAddr"
	return clientIPFor(r, trustXFF)
}

func (rl *Limiter) ClientIP(r *http.Request) string { // want ClientIP:"rawAddr"
	return clientIPFor(r, rl.trustXFF)
}

// IPKey is be0acf07's collapsing helper.
func IPKey(addr string) string { // want IPKey:"collapses"
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return addr
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	p, err := ip.WithZone("").Prefix(64)
	if err != nil {
		return addr
	}
	return p.String()
}

// ClientKey collapses through IPKey: credited by fact, not by name.
func (rl *Limiter) ClientKey(r *http.Request) string { // want ClientKey:"collapses"
	return IPKey(rl.ClientIP(r))
}

// preFixMiddleware is Middleware before be0acf07.
func (rl *Limiter) preFixMiddleware() func(http.Handler) http.Handler {
	return rl.MiddlewareByKey(func(r *http.Request) string { // want `ratelimitkey: this key function returns a client's full address and is passed to MiddlewareByKey`
		return rl.ClientIP(r)
	})
}

// Middleware is the fix spelling.
func (rl *Limiter) Middleware() func(http.Handler) http.Handler {
	return rl.MiddlewareByKey(rl.ClientKey)
}

// MiddlewareByKey's nil default before be0acf07 was rl.ClientIP.
func (rl *Limiter) MiddlewareByKey(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	if keyFunc == nil {
		keyFunc = rl.ClientKey
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, _ := rl.AllowContext(r.Context(), keyFunc(r)); !ok {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (rl *Limiter) preFixMiddlewareByKey(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	if keyFunc == nil {
		keyFunc = rl.ClientIP
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, _ := rl.AllowContext(r.Context(), keyFunc(r)); !ok { // want `ratelimitkey: the AllowContext key keyFunc\(r\) is a client's full address`
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
