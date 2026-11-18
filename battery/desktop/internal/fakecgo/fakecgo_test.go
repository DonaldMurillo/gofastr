//go:build darwin && (arm64 || amd64) && !cgo

package fakecgo_test

import (
	"os"
	"testing"

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
