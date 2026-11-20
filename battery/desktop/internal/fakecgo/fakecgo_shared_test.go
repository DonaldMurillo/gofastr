//go:build (darwin || linux) && (amd64 || arm64) && !cgo

package fakecgo_test

import (
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// getenv calls libc getenv and copies the C string out. The key is
// copied into a NUL-terminated Go buffer that stays referenced across
// the FFI call.
func getenv(t *testing.T, key string) string {
	t.Helper()
	b := make([]byte, len(key)+1)
	copy(b, key)
	r1, _, _ := ffi.Call(libcGetenv, []uintptr{uintptr(unsafe.Pointer(&b[0]))}, nil)
	runtime.KeepAlive(b)
	if r1 == 0 {
		return ""
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&r1))
	raw := unsafe.Slice((*byte)(p), 4096)
	n := 0
	for n < len(raw) && raw[n] != 0 {
		n++
	}
	return string(raw[:n])
}

// TestThreadsAndGC is the thread-start port under load. With iscgo true
// the runtime creates EVERY M through _cgo_thread_start
// (runtime/proc.go:2925), so 200 goroutines that lock their threads and
// call C in a loop force hundreds of real detached pthreads through
// x_cgo_sys_thread_start while the collector runs continuously. Without
// the fake-cgo layer this shape could not exist at all (cgocall threw);
// a broken thread-start port dies here with a fatal runtime error rather
// than failing an assertion.
func TestThreadsAndGC(t *testing.T) {
	// ThreadCreateProfile's ok is "the slice was large enough", not
	// "profiling is enabled": the records always exist.
	prof := make([]runtime.StackRecord, 1<<14)
	msBefore, ok := runtime.ThreadCreateProfile(prof)
	if !ok {
		t.Fatalf("thread creation profile overflowed %d records", len(prof))
	}
	var live atomic.Int64
	var peak atomic.Int64
	stop := make(chan struct{})
	var wgGC sync.WaitGroup
	wgGC.Add(1)
	go func() {
		defer wgGC.Done()
		cycles := 0
		for {
			select {
			case <-stop:
				t.Logf("GC goroutine: %d GC cycles", cycles)
				return
			default:
			}
			runtime.GC()
			debug.FreeOSMemory()
			cycles++
		}
	}()

	const n = 200
	var wg sync.WaitGroup
	started := make(chan struct{}, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer live.Add(-1)
			live.Add(1)
			if n := live.Load() + 1; n > peak.Load() {
				peak.Store(n)
			}
			runtime.LockOSThread()
			started <- struct{}{}
			for j := range 50 {
				r1, _, _ := ffi.Call(libcGetpid, nil, nil)
				if r1 == 0 {
					t.Error("getpid returned 0 mid-stress")
					return
				}
				// Allocate between C calls so the GC has young garbage
				// to collect while Ms churn through thread creation, and
				// pause a little so the 200 locked workers truly overlap
				// instead of draining serially.
				_ = strings.Repeat("x", j%64)
				time.Sleep(time.Millisecond)
			}
			runtime.UnlockOSThread()
		}()
	}

	// Wait until every goroutine has locked its thread. (Sampling
	// runtime.NumGoroutine here would be racy: workers that finish their
	// 50 calls exit immediately, so the count can already be back down;
	// concurrency is tracked by the peak counter below.)
	for range n {
		<-started
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("thread stress did not finish in 60s")
	}
	close(stop)
	wgGC.Wait()

	if p := peak.Load(); p < 100 {
		t.Errorf("peak concurrent locked workers = %d, want >= 100", p)
	}
	msAfter, _ := runtime.ThreadCreateProfile(prof)
	if d := msAfter - msBefore; d < 50 {
		t.Errorf("only %d new Ms created (profile %d -> %d); LockOSThread churn should force far more through _cgo_thread_start", d, msBefore, msAfter)
	}
}
