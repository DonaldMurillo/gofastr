package windows

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestClosedRefusalIsShared(t *testing.T) {
	err := withWindowHandle(901, true, func(uintptr) error {
		t.Fatal("closed window reached a native call")
		return nil
	})
	if err != desktop.ErrWindowClosed {
		t.Fatalf("destroyed handle error = %#v, want shared not_found error", err)
	}
}
