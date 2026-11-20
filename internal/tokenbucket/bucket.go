// Package tokenbucket provides the per-socket token-bucket rate limiter
// the realtime surfaces (rtc rooms, assist sockets) share: rate tokens
// per second, burst 2x rate, no initial leniency beyond the burst.
package tokenbucket

import "time"

// Bucket is a per-socket token bucket: rate tokens per second, burst 2x
// rate, no initial leniency beyond the burst.
type Bucket struct {
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

// New returns a Bucket refilling perSecond tokens per second with a burst
// of twice that, pre-filled to burst.
func New(perSecond int) Bucket {
	burst := 2 * float64(perSecond)
	return Bucket{rate: float64(perSecond), burst: burst, tokens: burst}
}

// Allow consumes one token at now and reports whether one was available.
func (b *Bucket) Allow(now time.Time) bool {
	if !b.last.IsZero() {
		if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
			b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
			b.last = now
		}
	} else {
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
