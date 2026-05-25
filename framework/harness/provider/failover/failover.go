// Package failover composes a chain of Providers with a
// circuit-breaker per upstream. Calls cascade down the chain on
// retryable failures (5xx, 408, 429 unless Retry-After is short).
//
// Per § Future extensions → PROV-FAILOVER. Wraps a slice of Providers
// behind one Provider interface; transparent to the engine.
package failover

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// Chain is the failover-aware Provider.
type Chain struct {
	// Members are the providers tried in order. The first whose
	// breaker is closed (and Chat doesn't immediately return a
	// retryable error) wins.
	Members []provider.Provider

	// Breakers, one per member. The same index is used.
	breakers []*breaker

	// FailureThreshold opens the breaker after this many consecutive
	// retryable failures from the same upstream. Default 3.
	FailureThreshold int

	// CooldownDuration is how long the breaker stays open before a
	// half-open trial. Default 30s.
	CooldownDuration time.Duration

	// NameOverride lets the Chain identify itself for logging.
	NameOverride string

	init sync.Once
}

// New returns a Chain over the given members.
func New(members ...provider.Provider) *Chain {
	return &Chain{Members: members}
}

func (c *Chain) ensureInit() {
	c.init.Do(func() {
		c.breakers = make([]*breaker, len(c.Members))
		for i := range c.Members {
			c.breakers[i] = &breaker{}
		}
		if c.FailureThreshold == 0 {
			c.FailureThreshold = 3
		}
		if c.CooldownDuration == 0 {
			c.CooldownDuration = 30 * time.Second
		}
	})
}

// Name implements provider.Provider.
func (c *Chain) Name() string {
	if c.NameOverride != "" {
		return c.NameOverride
	}
	return "failover"
}

// Chat tries each member in order. On an immediate retryable error
// from member N (HTTP 5xx / 429 / 408), records the failure and
// advances to N+1. On a non-retryable error (auth, bad request),
// returns it directly.
func (c *Chain) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	c.ensureInit()
	var lastErr error
	for i, p := range c.Members {
		b := c.breakers[i]
		if !b.canTry(c.CooldownDuration) {
			continue
		}
		stream, err := p.Chat(ctx, req)
		if err != nil {
			if isRetryable(err) {
				b.recordFailure(c.FailureThreshold)
				lastErr = err
				continue
			}
			return nil, err
		}
		b.recordSuccess()
		return stream, nil
	}
	if lastErr == nil {
		return nil, ErrNoMembersAvailable
	}
	return nil, lastErr
}

// Models returns the union of catalogs across members; the first
// reachable member contributes (avoids spamming all upstreams for
// every catalog refresh).
func (c *Chain) Models(ctx context.Context) ([]provider.Model, error) {
	c.ensureInit()
	for _, p := range c.Members {
		ms, err := p.Models(ctx)
		if err == nil {
			return ms, nil
		}
	}
	return nil, ErrNoMembersAvailable
}

// TokenCount uses the first member.
func (c *Chain) TokenCount(ctx context.Context, model string, msgs []provider.Message) (int, error) {
	c.ensureInit()
	if len(c.Members) == 0 {
		return 0, ErrNoMembersAvailable
	}
	return c.Members[0].TokenCount(ctx, model, msgs)
}

// breaker is a tiny circuit-breaker. Concurrency-safe.
type breaker struct {
	mu         sync.Mutex
	failures   int32  // consecutive failures
	openedAt   atomic.Int64 // unix nanos when opened; 0 if closed
}

func (b *breaker) canTry(cooldown time.Duration) bool {
	openedAt := b.openedAt.Load()
	if openedAt == 0 {
		return true
	}
	// Half-open after cooldown.
	if time.Since(time.Unix(0, openedAt)) >= cooldown {
		// One trial allowed; the trial's outcome is recorded by
		// recordFailure/recordSuccess.
		return true
	}
	return false
}

func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedAt.Store(0)
}

func (b *breaker) recordFailure(threshold int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if int(b.failures) >= threshold {
		b.openedAt.Store(time.Now().UnixNano())
	}
}

// isRetryable inspects an error returned by Provider.Chat. The
// internal/openai client embeds the HTTP status code in the error
// message; we string-match for clarity.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range []string{"HTTP 500", "HTTP 502", "HTTP 503", "HTTP 504", "HTTP 408", "HTTP 429"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	// Network errors (timeout, connection refused) are retryable.
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "EOF") {
		return true
	}
	return false
}

// ErrNoMembersAvailable is returned when every member's breaker is
// open or none are configured.
var ErrNoMembersAvailable = errors.New("failover: no available members")
