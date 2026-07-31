// Package clock provides a swap-able clock for tests. The
// conformance suite uses a fake clock for disconnect/reconnect/timeout
// scenarios so PR-gating tests don't depend on wall-clock waits.
package clock

import "time"

// Clock is the interface anything that needs time-of-day or Sleep
// uses. System returns the wall-clock implementation; Fake returns a
// manually-advanced one for tests.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// System returns the wall-clock implementation.
func System() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Now() time.Time        { return time.Now() }
func (systemClock) Sleep(d time.Duration) { time.Sleep(d) }

// Fake is a manually-advanced clock for deterministic tests.
type Fake struct {
	now time.Time
}

// NewFake returns a Fake clock starting at the given time.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the current fake time.
func (f *Fake) Now() time.Time { return f.now }

// Sleep advances the fake clock by d (returns immediately).
func (f *Fake) Sleep(d time.Duration) { f.now = f.now.Add(d) }

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.now = f.now.Add(d) }

// Compile-time assertion.
var _ Clock = (*Fake)(nil)
