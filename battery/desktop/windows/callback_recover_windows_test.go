//go:build windows && amd64

package windows

import (
	"testing"
	"time"
)

// A panicking menu, focus or frame callback drops its event; the process
// and later callbacks keep running.
func TestShellCallbackPanicIsContained(t *testing.T) {
	panicked := make(chan struct{})
	deliverOnGoroutine(func() {
		defer close(panicked)
		panic("host menu handler")
	})
	<-panicked
	next := make(chan struct{})
	deliverOnGoroutine(func() { close(next) })
	select {
	case <-next:
	case <-time.After(5 * time.Second):
		t.Fatal("the callback after a panicking one never ran")
	}
}
