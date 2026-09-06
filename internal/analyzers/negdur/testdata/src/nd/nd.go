// Package nd holds the negdur positives: synthetic spellings of the
// shapes the 2026-09-04 probes and the follow-up review found
// (TestSessionNegativeTTLFailsClosed,
// TestMemoryCacheNegativeTTLNotImmortal, the mutated-apitoken proof),
// with identifiers chosen away from the repo's sites. Nothing here
// rejects or clamps a negative on the decided value.
package nd

import (
	"errors"
	"time"
)

// p1 is the SUBSTITUTION shape: `d <= 0` folds a negative lifetime
// onto the default arm.
func p1(grant time.Duration) time.Duration {
	if grant <= 0 { // want `grant <= 0 folds a NEGATIVE duration onto the default arm`
		grant = 96 * time.Hour
	}
	return grant
}

// p2 is the `< 1` spelling of the same fold.
func p2(span time.Duration) time.Duration {
	if span < 1 { // want `span <= 0 folds a NEGATIVE`
		span = 30 * time.Minute
	}
	return span
}

// p3 is the reversed-operand spelling.
func p3(span time.Duration) time.Duration {
	if 0 >= span { // want `span <= 0 folds a NEGATIVE`
		span = 45 * time.Second
	}
	return span
}

// p4 substitutes a default-named field: same extension, other source.
type dialer struct {
	fallbackPause time.Duration
}

func (d *dialer) redial(pause time.Duration) time.Duration {
	if pause <= 0 { // want `pause <= 0 folds a NEGATIVE`
		pause = d.fallbackPause
	}
	return pause
}

// p5 is the NO-EXPIRY DECISION shape: `== 0` means default while
// `> 0` arms expiry, so a negative means forever (memory.go's Set).
type badge struct {
	expiresAt time.Time
	expiry    bool
}

type beacon struct {
	opts struct {
		fallbackHold time.Duration
	}
}

func (b *beacon) hold(window time.Duration) badge {
	v := window
	if v == 0 {
		v = b.opts.fallbackHold
	}
	return badge{expiry: v > 0} // want `v > 0 treats a NEGATIVE duration as no-expiry`
}

// p6 is the same decision in its statement spelling.
func (b *beacon) holdStmt(window time.Duration, now time.Time) *badge {
	v := window
	if v == 0 {
		v = b.opts.fallbackHold
	}
	bd := &badge{}
	if v > 0 { // want `v > 0 treats a NEGATIVE duration as no-expiry`
		bd.expiresAt = now.Add(v)
	}
	return bd
}

// p8 is the BARE no-expiry decision (posture (c), the reviewer's
// mutated-apitoken proof): `> 0` arms expiry with NO zero-default and
// no rejection anywhere — a negative silently means no expiry row.
type cred struct {
	expiresAt *time.Time
}

func p8(now time.Time, window time.Duration) cred {
	c := cred{}
	if window > 0 { // want `window > 0 arms expiry with no negative rejection in scope`
		exp := now.Add(window)
		c.expiresAt = &exp
	}
	return c
}

// p9 is the `!= 0` spelling of the same bare decision, with the
// time.After arm instead of an expiry-named assignment.
func p9(window time.Duration) <-chan time.Time {
	if window != 0 { // want `window > 0 arms expiry with no negative rejection in scope`
		return time.After(window)
	}
	return nil
}

// p7 is the `>= 1` decision spelling with the `<= 0` default.
func (b *beacon) holdGE(window time.Duration) badge {
	if window <= 0 { // want `window <= 0 folds a NEGATIVE`
		window = b.opts.fallbackHold
	}
	return badge{expiry: window >= 1} // want `window > 0 treats a NEGATIVE duration as no-expiry`
}

// p10: a validator IS called with the same struct, but it rejects a
// DIFFERENT duration field — the delegation does not cover the
// decided field, so the bare decision still fires.
type seat struct {
	hold     time.Duration
	refresh  time.Duration
	deadline *time.Time
}

func auditSeat(s seat) error {
	if s.refresh < 0 {
		return errors.New("seat refresh must not be negative")
	}
	return nil
}

func p10(s seat, now time.Time) seat {
	if err := auditSeat(s); err != nil {
		return s
	}
	if s.hold > 0 { // want `hold > 0 arms expiry with no negative rejection in scope`
		dl := now.Add(s.hold)
		s.deadline = &dl
	}
	return s
}
