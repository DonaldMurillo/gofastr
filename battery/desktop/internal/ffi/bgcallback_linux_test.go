//go:build linux && (amd64 || arm64)

package ffi

import (
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"unsafe"
)

// A callback delivered on a thread Go never created enters the runtime
// through needm on the extra M mstartm0 allocated at startup because
// iscgo was true (runtime/proc.go:1968-1970). Linux has no libdispatch,
// so the closest stand-in for the GIO worker or libnotify server thread
// a real app will see is a raw pthread whose start routine IS the
// trampoline: pthread_create takes a void*(*)(void*), which is exactly
// the callback signature, and pthread_join hands back the void* the
// callback returned.
//
// This pins four things at once: NewCallbackAnyThread skips the
// main-thread assertion; runtime.cgocallback survives needm/dropm on a
// thread with no g and no m; the callback's return value crosses back to
// C intact; and Go code inside such a callback can allocate and grow its
// stack hundreds of frames deep. That last one holds with or without
// fakecgo's _cgo_getstackbound — renaming that cell was tried, and this
// test stayed green — because the callback's frames live on a goroutine
// stack, not on the borrowed M's g0.
func TestCallbackFromForeignThread(t *testing.T) {
	ThreadCheck = func() { panic("ffi: ThreadCheck ran for an any-thread slot") }
	defer func() { ThreadCheck = nil }()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	caller, _, _ := Call(libc_pthread_self_trampoline_addr, nil, nil)
	if caller == 0 {
		t.Fatal("pthread_self returned 0 on the test thread")
	}

	var ran, foreign, grew atomic.Int64
	cb := NewCallbackAnyThread(func(a *Args) uintptr {
		tid, _, _ := Call(libc_pthread_self_trampoline_addr, nil, nil)
		if tid != caller {
			foreign.Add(1)
		}
		// Allocate and grow the goroutine stack on the borrowed M: the
		// interesting failures (bad g0 bounds, a half-entered M) show up
		// as a runtime throw right here, not as a wrong count.
		_ = make([]byte, 8<<10)
		if growStack(200) == 200 {
			grew.Add(1)
		}
		ran.Add(1)
		return a.Int[0] ^ 0xA5A5 // echo the pthread arg, transformed
	})

	stop := make(chan struct{})
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
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

	const waves, perWave = 8, 32
	const total = waves * perWave
	tids := make([]uintptr, perWave)
	rets := make([]uintptr, perWave)
	for w := range waves {
		for i := range perWave {
			arg := uintptr(w*perWave + i)
			rc, _, _ := Call(libc_pthread_create_trampoline_addr, []uintptr{
				uintptr(unsafe.Pointer(&tids[i])), 0, cb, arg,
			}, nil)
			runtime.KeepAlive(tids)
			if rc != 0 {
				t.Fatalf("pthread_create wave %d slot %d: errno %d", w, i, rc)
			}
		}
		for i := range perWave {
			rc, _, _ := Call(libc_pthread_join_trampoline_addr, []uintptr{
				tids[i], uintptr(unsafe.Pointer(&rets[i])),
			}, nil)
			runtime.KeepAlive(rets)
			if rc != 0 {
				t.Fatalf("pthread_join wave %d slot %d: errno %d", w, i, rc)
			}
			if want := uintptr(w*perWave+i) ^ 0xA5A5; rets[i] != want {
				t.Fatalf("thread %d returned %#x, want %#x: the callback's result did not reach the C caller", w*perWave+i, rets[i], want)
			}
		}
	}
	close(stop)
	<-gcDone

	if got := ran.Load(); got != total {
		t.Fatalf("ran %d callbacks, want %d", got, total)
	}
	if got := foreign.Load(); got != total {
		t.Fatalf("%d of %d callbacks reported the test thread's id; pthread_create did not deliver on a new thread", total-got, total)
	}
	if got := grew.Load(); got != total {
		t.Fatalf("only %d of %d callbacks grew a 200-frame Go stack on the borrowed M", got, total)
	}
}
