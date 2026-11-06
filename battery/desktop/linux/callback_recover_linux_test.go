//go:build linux && (amd64 || arm64)

package linux

import (
	"testing"
	"time"
)

// A panicking menu or settings callback drops its event; the process and
// later callbacks keep running.
func TestMenuCallbackPanicIsContained(t *testing.T) {
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
