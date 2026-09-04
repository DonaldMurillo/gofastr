// Package leaderish mirrors framework/cron's leader-election tick path,
// reduced to the shape: a timer-driven dispatch loop calling runTick,
// which acquires a lease through the package-declared Lease interface
// field (cron's LeaderElection, installed by the host via
// WithLeaderElection) and later invokes the returned release func on a
// bare goroutine. runTick is the pre-fix cron.go:151-179 (probes
// TestCronRedAcquirePanicIsolated, TestCronRedReleasePanicIsolated);
// runTickFixed is the fix posture, routing both host callbacks through
// recovered helpers. The rest pin the silent postures: stdlib
// interfaces, self-constructed implementations, and func results of
// package-local calls.
package leaderish

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// Lease is the extension point, exactly like cron.LeaderElection: the
// doc invites Redis/etcd implementations, all host code.
type Lease interface {
	Acquire(ctx context.Context) (held bool, release func(), err error)
}

type Scheduler struct {
	leader   Lease
	inflight sync.WaitGroup
	t        *time.Ticker
}

// run is the seed: a timer-driven dispatch loop (cron.go:318-338).
func (s *Scheduler) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-s.t.C:
			s.runTick(ctx, now)
		}
	}
}

// runTick is the pre-fix shape: Acquire runs inline on the loop
// goroutine (cron.go:156) and the returned release runs on a bare
// goroutine once the tick's jobs finish (cron.go:171-176), neither with
// a recover between it and the process.
func (s *Scheduler) runTick(ctx context.Context, now time.Time) bool {
	held, release, err := s.leader.Acquire(ctx) // want `recovercallback: s\.leader\.Acquire is invoked with no recover in scope`
	if err != nil {
		return false
	}
	if !held {
		return false
	}
	defer func() {
		go func() {
			s.inflight.Wait()
			release() // want `recovercallback: release is invoked with no recover in scope`
		}()
	}()
	s.fire(now)
	return true
}

func (s *Scheduler) fire(now time.Time) { _ = now }

// runTickFixed is the fix posture: Acquire runs inside a recovered
// helper that routes the panic, and the release goroutine gets the same
// net (the reportError/gateAllow precedent cron already ships).
func (s *Scheduler) runTickFixed(ctx context.Context, now time.Time) bool {
	held, release, err := s.acquireSafe(ctx)
	if err != nil || !held {
		return false
	}
	defer func() {
		go s.releaseSafe(release)
	}()
	s.fire(now)
	return true
}

func (s *Scheduler) acquireSafe(ctx context.Context) (held bool, release func(), err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("lease acquire panicked")
		}
	}()
	return s.leader.Acquire(ctx)
}

func (s *Scheduler) releaseSafe(release func()) {
	defer func() {
		_ = recover()
	}()
	s.inflight.Wait()
	release()
}

// runFixed drives runTickFixed from the same timer loop, so the fix
// posture above is on a hot path exactly like the pre-fix shape.
func (s *Scheduler) runFixed(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-s.t.C:
			s.runTickFixed(ctx, now)
		}
	}
}

// stdioLoop stays quiet twice over: io.Reader is stdlib (data plumbing,
// not an app callback) and Read is the loop's own input call, the same
// name family the read-loop detector uses.
type stdioLoop struct {
	r io.Reader
	c io.Closer
}

func (l *stdioLoop) run(buf []byte) {
	for {
		if _, err := l.r.Read(buf); err != nil {
			return
		}
		// Close is not an input name, so the module criterion is the
		// only thing keeping this quiet: a stdlib interface is never an
		// extension point.
		_ = l.c.Close()
	}
}

// localLease implements Lease inside the package.
type localLease struct{}

func (localLease) Acquire(context.Context) (bool, func(), error) {
	return true, func() {}, nil
}

// selfConstructed stays quiet: the package built the implementation
// itself (static type is the interface, value is local), so the call is
// package-chosen code, and the edge flood already reaches
// localLease.Acquire.
func selfConstructed(ctx context.Context) {
	var le Lease = localLease{}
	go func() {
		for {
			_, _, _ = le.Acquire(ctx)
		}
	}()
}

// inlineIface stays quiet: an unnamed inline interface field has no
// declaring package to judge, so it is never an extension point.
type inlineIface struct {
	r interface{ Refresh() error }
}

func (in *inlineIface) driven() {
	go func() {
		for {
			_ = in.r.Refresh()
		}
	}()
}

// acquireLocal is a package-local call returning the same shape as
// Acquire. Its release is NOT a host-built func, so invoking it on a
// goroutine stays quiet: the func-result leg only tracks results of
// callback calls.
func acquireLocal(ctx context.Context) (bool, func(), error) {
	return true, func() { _ = ctx }, nil
}

func localResultDriven(ctx context.Context) {
	_, rel, _ := acquireLocal(ctx)
	go func() {
		rel()
	}()
}
