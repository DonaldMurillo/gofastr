//go:build (darwin || linux) && (amd64 || arm64)

package ffi

import "testing"

// TestNewCallbackSlotExhaustion pins the fixed-slot contract: the
// registry is bounded by MaxCallbacks and says so loudly. Pure Go: no C
// call. The registry is implemented per OS (callback_darwin.go,
// callback_linux.go); the contract is one, so the test lives once.
func TestNewCallbackSlotExhaustion(t *testing.T) {
	used := callbackLen
	for callbackLen < MaxCallbacks {
		NewCallback(func(a *Args) uintptr { return 0 })
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewCallback did not panic at slot exhaustion")
		} else if _, ok := r.(string); !ok {
			t.Errorf("panic value is %T (%v), want string", r, r)
		}
		callbackLen = used // not a real free; keeps other tests runnable
	}()
	NewCallback(func(a *Args) uintptr { return 0 })
}
