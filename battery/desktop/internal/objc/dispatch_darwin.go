//go:build darwin && (arm64 || amd64)

package objc

import (
	"fmt"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// mainThreadID is the pthread_t of the OS thread the main goroutine
// locked in init. Every AppKit and WebKit call and every callback must
// run on it; Main is the only way other goroutines get there.
var mainThreadID uintptr

// MainThreadID returns the locked main thread's pthread_t.
func MainThreadID() uintptr { return mainThreadID }

// AssertMainThread panics when the caller is not on the main thread.
// The what string names the operation for the panic message.
func AssertMainThread(what string) {
	if PthreadSelf() != mainThreadID {
		panic(fmt.Sprintf("objc: %s called on thread %#x, want main thread %#x", what, PthreadSelf(), mainThreadID))
	}
}

var (
	mainMu  sync.Mutex
	mainFns []*mainJob
)

// mainTrampoline is the single dispatch_async_f function pointer; its
// context is always nil because the work travels through mainFns.
var mainTrampoline = ffi.NewCallback(func(a *ffi.Args) uintptr {
	AssertMainThread("dispatch main-queue drain")
	for {
		mainMu.Lock()
		if len(mainFns) == 0 {
			mainMu.Unlock()
			return 0
		}
		job := mainFns[0]
		mainFns = mainFns[1:]
		mainMu.Unlock()
		job.fn()
	}
})

// DefaultMainTimeout bounds a Main call that does not name its own
// deadline. It suits work that returns on its own; work that waits for
// a HUMAN must name a longer one through MainWithTimeout.
const DefaultMainTimeout = 10 * time.Second

// Main runs fn on the main thread and waits for it to finish, with the
// DefaultMainTimeout deadline. It works while [NSApp run] owns the main
// thread (the run loop services the dispatch main queue between events)
// and before the run loop starts (dispatch drains immediately).
func Main(fn func()) error {
	return MainWithTimeout(DefaultMainTimeout, fn)
}

// MainWithTimeout is Main with a caller-chosen deadline.
//
// The deadline belongs to the caller because only the caller knows
// whether the work waits for a person. A fixed 10 seconds here silently
// voided every permission alert a user took longer than that to answer:
// NSAlert's runModal blocks until the click, Main gave up first, and the
// bridge turned that into an internal error that persisted NO decision —
// so the page could ask again, which is the exact loop the persisted
// deny exists to stop.
//
// On timeout the queued closure is REMOVED from the queue, not merely
// abandoned. Leaving it there meant it ran later (when the run loop
// finally drained), wrote through the caller's captured variables after
// the caller had returned, and stacked one more modal alert behind the
// one already on screen for every retry.
// A closure that has ALREADY been taken by the drain loop cannot be
// recalled; the deadline still expires, and the caller must therefore
// treat everything the closure writes as unsafe to read. Prompt does
// this by writing its result through a mutex rather than a captured
// local.
func MainWithTimeout(d time.Duration, fn func()) error {
	ensureRuntime()
	done := make(chan struct{})
	job := &mainJob{fn: func() {
		defer close(done)
		fn()
	}}
	mainMu.Lock()
	mainFns = append(mainFns, job)
	mainMu.Unlock()
	// dispatch_async_f(queue, context, function): symDispatchMainQ is
	// the address of the _dispatch_main_q data symbol, which IS the
	// queue object.
	_, _, _ = ffi.Call(symDispatchAsyncF, []uintptr{symDispatchMainQ, 0, mainTrampoline}, nil)
	select {
	case <-done:
		return nil
	case <-time.After(d):
		dropQueuedMain(job)
		return fmt.Errorf("objc: Main timed out after %s waiting for the main thread", d)
	}
}

// mainJob wraps queued work so the queue can be searched by POINTER
// identity. Two calls of the same closure literal share a code pointer,
// so reflect-based matching would drop the wrong one.
type mainJob struct{ fn func() }

// dropQueuedMain removes job from the pending queue, reporting whether
// it was still there. False means the drain loop already took it.
func dropQueuedMain(job *mainJob) bool {
	mainMu.Lock()
	defer mainMu.Unlock()
	for i, j := range mainFns {
		if j == job {
			mainFns = append(mainFns[:i], mainFns[i+1:]...)
			return true
		}
	}
	return false
}
