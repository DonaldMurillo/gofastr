//go:build darwin && (arm64 || amd64)

package objc

import (
	"fmt"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

var mainThreadID uintptr

// MainThreadID returns the locked main thread's pthread_t.
func MainThreadID() uintptr { return mainThreadID }

// AssertMainThread panics when the caller is not on the main thread.
func AssertMainThread(what string) {
	if PthreadSelf() != mainThreadID {
		panic(fmt.Sprintf("objc: %s called on thread %#x, want main thread %#x", what, PthreadSelf(), mainThreadID))
	}
}

var (
	mainMu  sync.Mutex
	mainFns []*mainJob
)

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

// DefaultMainTimeout bounds a Main call that does not name its own deadline.
const DefaultMainTimeout = 10 * time.Second

// Main runs fn on the main thread and waits for it to finish, with the DefaultMainTimeout deadline.
func Main(fn func()) error {
	return MainWithTimeout(DefaultMainTimeout, fn)
}

// MainWithTimeout is Main with a caller-chosen deadline.
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
	_, _, _ = ffi.Call(symDispatchAsyncF, []uintptr{symDispatchMainQ, 0, mainTrampoline}, nil)
	select {
	case <-done:
		return nil
	case <-time.After(d):
		dropQueuedMain(job)
		return fmt.Errorf("objc: Main timed out after %s waiting for the main thread", d)
	}
}

type mainJob struct{ fn func() }

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
