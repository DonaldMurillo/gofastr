package linux

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestCloseIsIdempotent(t *testing.T) {
	stopped := errors.New("dispatcher stopped")
	for _, test := range []struct {
		name         string
		closed       bool
		closeOnQueue bool
		dispatchErr  error
		wantCalls    int
	}{
		{name: "live", wantCalls: 1},
		{name: "already closed", closed: true},
		{name: "closes before UI dispatch", closeOnQueue: true},
		{name: "stopped", dispatchErr: stopped},
	} {
		t.Run(test.name, func(t *testing.T) {
			closed, calls := test.closed, 0
			err := closeNativeWindow(func() bool { return closed }, func(action func()) error {
				if test.dispatchErr != nil {
					return test.dispatchErr
				}
				closed = closed || test.closeOnQueue
				action()
				return nil
			}, func() { calls++ })
			if err != test.dispatchErr || calls != test.wantCalls {
				t.Fatalf("Close error=%v calls=%d; want error=%v calls=%d", err, calls, test.dispatchErr, test.wantCalls)
			}
		})
	}
	if err := errClosed(); err != desktop.ErrWindowClosed {
		t.Fatalf("closed refusal = %#v, want shared error", err)
	}
}
