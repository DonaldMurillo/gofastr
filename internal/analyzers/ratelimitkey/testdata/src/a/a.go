package a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"ratelimit"
)

// RateLimiter mirrors battery/auth's wrapper.
type RateLimiter struct {
	*ratelimit.Limiter
}

// preFixClientIP is battery/auth's clientIP before be0acf07.
func (rl *RateLimiter) preFixClientIP(r *http.Request) string { // want preFixClientIP:"rawAddr"
	return rl.Limiter.ClientIP(r)
}

func (rl *RateLimiter) clientIP(r *http.Request) string { // want clientIP:"collapses"
	return rl.Limiter.ClientKey(r)
}

func (rl *RateLimiter) preFixGuard(r *http.Request) bool {
	ok, _ := rl.AllowContext(r.Context(), rl.preFixClientIP(r)) // want `ratelimitkey: the AllowContext key rl.preFixClientIP\(r\) is a client's full address`
	return ok
}

func (rl *RateLimiter) guard(r *http.Request) bool {
	ok, _ := rl.AllowContext(r.Context(), rl.clientIP(r))
	return ok
}

// prefixedKey concatenates a scope onto the raw address: still raw.
func (rl *RateLimiter) prefixedKey(r *http.Request) bool {
	ok, _ := rl.Allow("login:" + ratelimit.ClientIP(r, false)) // want `ratelimitkey: the Allow key "login:" \+ ratelimit.ClientIP\(r, false\)`
	return ok
}

func (rl *RateLimiter) prefixedFixed(r *http.Request) bool {
	ok, _ := rl.Allow("login:" + ratelimit.IPKey(ratelimit.ClientIP(r, false)))
	return ok
}

// RateLimitKey mirrors core/webbotauth: an agent key, else the fallback.
func RateLimitKey(fallback func(*http.Request) string) func(*http.Request) string {
	if fallback == nil {
		fallback = remoteHost
	}
	return func(r *http.Request) string { // want `ratelimitkey: this key function returns a client's full address and is returned as a rate-limit key function`
		if a := r.Header.Get("Signature-Agent"); a != "" {
			return "agent:" + a
		}
		return fallback(r)
	}
}

func remoteHost(r *http.Request) string { // want remoteHost:"rawAddr"
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// preFixAgentKey is framework.AgentRateLimitKey before be0acf07.
func preFixAgentKey(trustXFF bool) func(*http.Request) string {
	return RateLimitKey(func(r *http.Request) string { // want `ratelimitkey: this key function returns a client's full address and is passed to RateLimitKey`
		return ratelimit.ClientIP(r, trustXFF)
	})
}

func agentKey(trustXFF bool) func(*http.Request) string {
	return RateLimitKey(func(r *http.Request) string {
		return ratelimit.IPKey(ratelimit.ClientIP(r, trustXFF))
	})
}

// Config mirrors core/middleware RateLimitConfig.
type Config struct {
	KeyFunc func(*http.Request) string
}

func defaultKey(r *http.Request) string { // want defaultKey:"rawAddr"
	return stripPort(r.RemoteAddr)
}

func stripPort(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func configure(cfg Config) Config {
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = defaultKey // want `ratelimitkey: defaultKey returns a client's full address and is the KeyFunc`
	}
	return cfg
}

func literalConfig() Config {
	return Config{KeyFunc: func(r *http.Request) string { // want `ratelimitkey: this key function returns a client's full address and is the KeyFunc`
		return r.RemoteAddr
	}}
}

// userKey keys on the user, not the address.
func userKey() Config {
	return Config{KeyFunc: func(r *http.Request) string {
		return r.Header.Get("X-User")
	}}
}

// Near miss: collapsing a value it then drops credits nothing; the
// result is still the full address.
func collapsesAndDrops(r *http.Request, log func(string)) string { // want collapsesAndDrops:"rawAddr"
	log(ratelimit.IPKey(r.RemoteAddr))
	return r.RemoteAddr
}

func (rl *RateLimiter) usesDropped(r *http.Request, log func(string)) bool {
	ok, _ := rl.Allow(collapsesAndDrops(r, log)) // want `ratelimitkey: the Allow key collapsesAndDrops\(r, log\)`
	return ok
}

// logs the full address: not a key.
func logsAddress(r *http.Request, log func(string)) {
	log(fmt.Sprintf("from %s", ratelimit.ClientIP(r, false)))
}

// Near miss: an Allow method on a type that is not a limiter.
type Policy struct{}

func (Policy) Allow(key string) bool { return key != "" }

func policyCheck(p Policy, r *http.Request) bool {
	return p.Allow(r.RemoteAddr)
}

// Near miss: a caller-supplied key function is the caller's to judge.
func custom(rl *ratelimit.Limiter, key func(*http.Request) string, r *http.Request) bool {
	ok, _ := rl.Allow(key(r))
	return ok
}

// A store with a context-taking Allow: still a limiter.
type SQLRateLimitStore struct{}

func (SQLRateLimitStore) Allow(ctx context.Context, key string, window time.Duration) (bool, error) {
	return true, nil
}

func sqlStore(s SQLRateLimitStore, r *http.Request) {
	_, _ = s.Allow(r.Context(), remoteHost(r), time.Minute) // want `ratelimitkey: the Allow key remoteHost\(r\)`
}
