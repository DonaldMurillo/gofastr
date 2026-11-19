//go:build darwin && (amd64 || arm64)

package ffi

import "testing"

// TestCallRejectsTooManyArgs pins the other half of the stackargs fix
// proven per-arch by TestStackArgsReachC: past the MaxIntArgs integer
// or MaxFloatArgs float slots the callArgs block can carry, Call says
// so instead of silently dropping arguments on the floor. The limits
// are per-arch constants (IntRegs registers plus the eight stack words;
// eight float registers); the contract is one, so the test lives once.
// The trampoline address is never reached — the panic fires first.
func TestCallRejectsTooManyArgs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ints   []uintptr
		floats []float64
	}{
		{"ints", make([]uintptr, MaxIntArgs+1), nil},
		{"floats", nil, make([]float64, MaxFloatArgs+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Call accepted more arguments than the block can carry")
				}
			}()
			Call(libc_pthread_self_trampoline_addr, tc.ints, tc.floats)
		})
	}
}
