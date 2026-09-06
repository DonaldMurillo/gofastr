//go:build darwin && (arm64 || amd64)

package ffi

import (
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"
)

// A callback delivered on a thread Go never created (a dispatch global
// queue worker) enters the runtime through needm on the extra M the
// fake-cgo layer had it allocate at startup. UNUserNotificationCenter's
// completion handlers arrive exactly this way in a bundled app, and the
// first bundled build died there because every slot asserted the main
// thread. This pins both halves: NewCallbackAnyThread skips the
// assertion, and the runtime survives thousands of such entries under
// GC pressure.
func TestCallbackFromForeignThread(t *testing.T) {
	ThreadCheck = func() { panic("ffi: ThreadCheck ran for an any-thread slot") }
	defer func() { ThreadCheck = nil }()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	caller, _, _ := Call(libc_pthread_self_trampoline_addr, nil, nil)

	var ran atomic.Int64
	var foreign atomic.Int64
	done := make(chan struct{}, 4096)
	cb := NewCallbackAnyThread(func(a *Args) uintptr {
		tid, _, _ := Call(libc_pthread_self_trampoline_addr, nil, nil)
		if tid != caller {
			foreign.Add(1)
		}
		ran.Add(1)
		done <- struct{}{}
		return 0
	})

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
				debug.FreeOSMemory()
			}
		}
	}()
	defer close(stop)

	q, _, _ := Call(libc_dispatch_get_global_queue_trampoline_addr, []uintptr{0, 0}, nil)
	if q == 0 {
		t.Fatal("dispatch_get_global_queue returned nil")
	}
	const n = 2000
	for i := 0; i < n; i++ {
		Call(libc_dispatch_async_f_trampoline_addr, []uintptr{q, uintptr(i), cb}, nil)
	}
	deadline := time.After(30 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-deadline:
			t.Fatalf("only %d of %d foreign-thread callbacks arrived", ran.Load(), n)
		}
	}
	if got := ran.Load(); got != n {
		t.Fatalf("ran %d callbacks, want %d", got, n)
	}
	if foreign.Load() == 0 {
		t.Fatal("every callback ran on the dispatching thread; the global queue did not deliver on a foreign thread")
	}
}
