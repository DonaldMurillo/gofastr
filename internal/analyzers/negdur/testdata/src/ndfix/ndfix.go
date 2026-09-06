// Package ndfix holds the negdur silent postures, one fixture per
// posture: rejection, refusal, clamp, non-extending substitution,
// non-caller-supplied durations, the two "no in-function inversion"
// shapes (redis.go Set's pass-through, the validator-backed
// apitoken.go IssueToken decision), and the developer-configuration
// subjects (receiver fields, config-named parameter types, nested
// options hops).
package ndfix

import (
	"errors"
	"time"
)

// n1: a dominating `d < 0` rejection — negatives get their own arm
// before the zero-default runs.
func n1(grant time.Duration) time.Duration {
	if grant < 0 {
		return 0
	}
	if grant <= 0 {
		grant = 24 * time.Hour
	}
	return grant
}

// n2: `d >= 0` in a guard position rejects negatives in the else arm.
func n2(grant time.Duration) time.Duration {
	if grant >= 0 && grant < 48*time.Hour {
		return grant
	}
	if grant <= 0 {
		grant = 24 * time.Hour
	}
	return grant
}

// n3: refusal instead of substitution (EntitySessionStore.Create's
// posture): the <= 0 arm diverges with an error.
func n3(grant time.Duration) error {
	if grant <= 0 {
		return errors.New("grant must be positive")
	}
	return nil
}

// n4: clamp-to-zero (already-expired) before defaulting.
func n4(grant time.Duration) time.Duration {
	if grant < 0 {
		grant = 0
	}
	if grant == 0 {
		grant = time.Hour
	}
	return grant
}

// n5: the max() clamp spelling.
func n5(grant time.Duration) time.Duration {
	grant = max(grant, 0)
	if grant <= 0 {
		grant = time.Hour
	}
	return grant
}

// n6: the substituted value is itself zero — no lifetime extension.
func n6(grant time.Duration) time.Duration {
	if grant <= 0 {
		grant = 0
	}
	return grant
}

// leaseOff is the code's own "disabled" vocabulary: substituting it is
// a contract, not an extension.
var leaseOff time.Duration

func n7(grant time.Duration) time.Duration {
	if grant <= 0 {
		grant = leaseOff
	}
	return grant
}

// n8: a constant duration is not caller-supplied.
const fixedHold = 2 * time.Hour

func n8() time.Duration {
	w := fixedHold
	if w <= 0 {
		w = time.Hour
	}
	return w
}

// n9: time.Since results are not caller-supplied.
func n9(start time.Time) bool {
	age := time.Since(start)
	if age <= 0 {
		age = time.Second
	}
	return age > 0
}

// n10: a bare `== 0` default with no in-function no-expiry decision
// (redis.go Set): the negative keeps its sign to the downstream call.
func n10(sink func(time.Duration), window time.Duration) {
	if window == 0 {
		window = 15 * time.Minute
	}
	sink(window)
}

// n11 (flipped by the bare-decision posture): `> 0` arms expiry with
// no zero-default, but the validator called with the same value
// rejects the negative first — the rejection is in scope through the
// helper (apitoken.go IssueToken → validateTokenSpec). Delete the
// helper's rejection and this is p8's shape.
type lease struct {
	expiresAt *time.Time
	window    time.Duration
}

func checkLease(l lease) error {
	if l.window < 0 {
		return errors.New("lease window must not be negative")
	}
	return nil
}

func n11(l lease, now time.Time) lease {
	if err := checkLease(l); err != nil {
		return l
	}
	if l.window > 0 {
		exp := now.Add(l.window)
		l.expiresAt = &exp
	}
	return l
}

// n12: a subject read off the RECEIVER is developer configuration
// ((c *AuthConfig).defaults posture): a host author writing a
// negative is a footgun, not an attacker-reachable inversion.
type sweeper struct {
	period time.Duration
}

func (s *sweeper) n12() {
	if s.period <= 0 {
		s.period = time.Hour
	}
}

// n13: a subject read off a configuration-named parameter type is
// developer configuration too (NewPasswordResetPlugin(cfg …Config)
// posture), at the first hop.
type janitorConfig struct {
	every time.Duration
}

func n13(cfg janitorConfig) janitorConfig {
	if cfg.every <= 0 {
		cfg.every = time.Minute
	}
	return cfg
}

// n14: the same posture at a NESTED hop — the duration's base is a
// field of an options-typed struct (supervisor `w.opts.Tick` shape).
type supervisorOpts struct {
	tick time.Duration
}

type worker struct {
	opts supervisorOpts
}

func n14(w *worker) {
	if w.opts.tick <= 0 {
		w.opts.tick = 500 * time.Millisecond
	}
}
