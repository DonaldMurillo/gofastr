package windows

import (
	"errors"
	"testing"
)

func TestClosedHandlesRefuseNativeCalls(t *testing.T) {
	for _, test := range []struct {
		name   string
		handle uintptr
		closed bool
		open   bool
	}{
		{name: "uninitialized"},
		{name: "destroyed", handle: 42, closed: true},
		{name: "recycled", handle: 901, closed: true},
		{name: "open", handle: 77, open: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			nativeErr := errors.New("native failure")
			err := withWindowHandle(test.handle, test.closed, func(handle uintptr) error {
				calls++
				if handle != test.handle {
					t.Errorf("native handle = %d, want %d", handle, test.handle)
				}
				return nativeErr
			})
			if test.open {
				if calls != 1 || err != nativeErr {
					t.Fatalf("live handle: calls=%d error=%v", calls, err)
				}
			} else if calls != 0 || err == nil {
				t.Fatalf("closed handle reached native API: calls=%d error=%v", calls, err)
			}
		})
	}
}
