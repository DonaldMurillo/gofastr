// Package bd holds the bareduration fixtures. The pre-fix shape is the
// caller of 7615b2bd2's in-memory queue.Scheduler: Every(5) compiled as
// five nanoseconds and the timer loop spun a job per iteration.
package bd

import (
	"math"
	"time"
)

type Scheduler struct{ interval time.Duration }

func (s *Scheduler) Every(d time.Duration) *Scheduler { s.interval = d; return s }

func variadic(ds ...time.Duration) {}

type Config struct {
	Timeout time.Duration
	Retries int
}

const pollSeconds = 5             // untyped: still a count
const tick time.Duration = 5      // typed: a choice the declaration states
const poll = 5 * time.Millisecond // typed by its unit
const backoffBase = 2             // untyped multiplier

// Pre-fix: the 7615b2bd2 caller.
func preFix(s *Scheduler) {
	s.Every(5) // want `bare integer 5 converted to time.Duration is 5 nanoseconds`
}

func sinks(s *Scheduler, ch chan time.Duration) time.Duration {
	s.Every(pollSeconds)                // want `bare integer pollSeconds converted to time.Duration is 5 nanoseconds`
	s.Every(60 * 60)                    // want `bare integer 60 \* 60 converted to time.Duration is 3600 nanoseconds`
	variadic(time.Second, 30)           // want `bare integer 30 converted`
	_ = Config{Timeout: 30, Retries: 3} // want `bare integer 30 converted`
	_ = []time.Duration{1, time.Second} // want `bare integer 1 converted`
	var d time.Duration = 10            // want `bare integer 10 converted`
	d = 15                              // want `bare integer 15 converted`
	ch <- 20                            // want `bare integer 20 converted`
	_ = d
	return 100 // want `bare integer 100 converted`
}

// The fix spelling and every silent posture.
func quiet(s *Scheduler, d time.Duration) time.Duration {
	s.Every(5 * time.Second)                  // fix: the unit is stated
	s.Every(time.Duration(5))                 // explicit conversion
	s.Every(0)                                // zero is "unset"
	s.Every(-1)                               // a sentinel
	s.Every(tick)                             // named Duration constant
	s.Every(poll)                             // typed by its unit
	s.Every(backoffBase * time.Second)        // untyped multiplier of a unit
	s.Every(d + 5)                            // an offset on a typed value
	s.Every(time.Duration(math.MaxInt64))     // explicit
	var forever time.Duration = math.MaxInt64 // "forever" sentinel
	_ = Config{Retries: 3}                    // an int field: not a Duration
	n := 5                                    // an int local: not a Duration
	_ = n
	return forever
}

// Near-miss: a Duration-typed comparison or arithmetic with a bare
// integer is not one of the sinks — `d > 5` and `d * 2` do not hand 5
// or 2 to anything as a duration.
func nearMiss(d time.Duration) bool {
	_ = d * 2
	d *= 2 // core/backoff's doubling
	d += 5
	return d > 5
}
