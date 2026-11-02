package oz

import (
	"errors"
	"os"
	"sync/atomic"
	"time"
)

// The fix spellings: keep the default on a non-positive value, or
// refuse it.

type Fixed struct {
	lease      time.Duration
	visibility atomic.Int64
	attempts   int
	budget     time.Duration
}

type FixedOption func(*Fixed)

func NewFixed() *Fixed {
	f := &Fixed{lease: 5 * time.Minute, attempts: 10, budget: time.Minute}
	f.visibility.Store(int64(30 * time.Second))
	return f
}

func WithFixedLease(d time.Duration) FixedOption {
	return func(f *Fixed) {
		if d > 0 {
			f.lease = d
		}
	}
}

func (f *Fixed) SetFixedVisibility(d time.Duration) {
	if d <= 0 {
		return
	}
	f.visibility.Store(int64(d))
}

func WithFixedAttempts(n int) FixedOption {
	if n <= 0 {
		panic(errors.New("attempts must be positive"))
	}
	return func(f *Fixed) { f.attempts = n }
}

// A validator taking the parameter is a check.
func WithFixedBudget(d time.Duration) FixedOption {
	validate(d)
	return func(f *Fixed) { f.budget = d }
}

func validate(time.Duration) {}

// Use-site guard: the field is compared to 0 where it is read
// (framework/export_data.go pageSize, acp maxTurns, cache
// cleanupInterval), so zero is handled downstream.

type Exporter struct {
	pageSize int
	interval time.Duration
	turns    atomic.Int64
}

func NewExporter() *Exporter {
	e := &Exporter{pageSize: 500, interval: time.Minute}
	e.turns.Store(8)
	return e
}

func WithPageSize(n int) func(*Exporter) {
	return func(e *Exporter) { e.pageSize = n }
}

func WithInterval(d time.Duration) func(*Exporter) {
	return func(e *Exporter) { e.interval = d }
}

func (e *Exporter) SetTurns(n int) { e.turns.Store(int64(n)) }

func (e *Exporter) run() {
	size := e.pageSize
	if size <= 0 {
		size = 500
	}
	_ = size
	if e.interval > 0 {
		_ = time.NewTicker(e.interval)
	}
	if e.turns.Load() < 1 {
		return
	}
}

// Unexported setter: the caller is the package itself.
func withLease(d time.Duration) FixedOption {
	return func(f *Fixed) { f.lease = d }
}

// Near-misses on the name: Settle and Without are not With*/Set*
// setters, and a bool parameter is not numeric.
func (f *Fixed) Settle(n int) { f.attempts = n }

func WithoutBudget(d time.Duration) FixedOption {
	return func(f *Fixed) { f.budget = d }
}

func WithDebug(b bool) FixedOption { return func(*Fixed) { _ = b } }

// A named integer type other than Duration is a value set, not a
// count: a zero FileMode is a mode (battery/storage WithPermissions).
type Local struct{ fileMode os.FileMode }

func NewLocal() *Local { return &Local{fileMode: 0o600} }

func WithPermissions(mode os.FileMode) func(*Local) {
	return func(l *Local) { l.fileMode = mode }
}
