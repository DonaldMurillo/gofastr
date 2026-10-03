//go:build darwin && (arm64 || amd64)

package fakecgo_test

import (
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/fakecgo"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// runtime_iscgo is runtime.iscgo read back through the same pushed
// linkname fakecgo itself uses.
//
//go:linkname runtime_iscgo runtime.iscgo
var runtime_iscgo bool

// C function addresses come from fakecgo's assembly trampolines: a
// linknamed byte var over a cgo_import_dynamic symbol does not bind
// without a host object, so the address must originate in a .s file.
var (
	libcGetpid = fakecgo.LibcGetpidAddr()
	libcGetenv = fakecgo.LibcGetenvAddr()
)

// TestIscgoAndInit pins the two facts the rest of the desktop stack
// depends on: importing fakecgo flips runtime.iscgo before any package
// init code runs ffi.Call, and rt0_go actually found and called our
// _cgo_init (a non-zero setg cell is the witness — nothing else writes
// it, and it happens before any Go code exists).
func TestIscgoAndInit(t *testing.T) {
	if !runtime_iscgo {
		t.Fatal("runtime.iscgo is false: fakecgo's init did not run or did not take effect")
	}
	if fakecgo.SetgStored() == 0 {
		t.Fatal("setg_gcc cell is zero: rt0_go never called x_cgo_init (the _cgo_init cell did not bind)")
	}
	// And the point of iscgo: cgocall must not throw "cgocall
	// unavailable". One trivial call through the public FFI.
	r1, _, _ := ffi.Call(libcGetpid, nil, nil)
	if r1 == 0 {
		t.Fatalf("getpid through ffi.Call returned 0")
	}
}

// TestSetenvRoundTrip proves os.Setenv reached libc's environ through
// our x_cgo_setenv (runtime setenv_c calls it via asmcgocall whenever
// _cgo_setenv is non-nil): a C getenv in the same process sees the
// value, and os.Unsetenv removes it from the C side too. Without the
// fake-cgo layer, _cgo_setenv is nil and setenv_c is a no-op, so libc's
// environ would never see the key.
func TestSetenvRoundTrip(t *testing.T) {
	const k = "GOFASTR_FAKECGO_PROBE"
	os.Setenv(k, "set through Go")
	got := getenv(t, k)
	if got != "set through Go" {
		t.Fatalf("C getenv(%q) = %q, want %q: os.Setenv did not reach libc environ", k, got, "set through Go")
	}
	os.Unsetenv(k)
	if got := getenv(t, k); got != "" {
		t.Fatalf("C getenv(%q) = %q after os.Unsetenv, want \"\"", k, got)
	}
}

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

// fake-cgo layer this shape could not exist at all (cgocall threw);
// a broken thread-start port dies here with a fatal runtime error
// rather than failing an assertion.
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
				// to collect while Ms churn through thread creation,
				// and pause a little so the 200 locked workers truly
				// overlap instead of draining serially.
				_ = strings.Repeat("x", j%64)
				time.Sleep(time.Millisecond)
			}
			runtime.UnlockOSThread()
		}()
	}

	// Wait until every goroutine has locked its thread. (Sampling
	// runtime.NumGoroutine here would be racy: workers that finish
	// their 50 calls exit immediately, so the count can already be
	// back down; concurrency is tracked by the peak counter below.)
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
