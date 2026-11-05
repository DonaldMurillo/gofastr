package macos

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestClosedWindowUsesNotFound(t *testing.T) {
	if err := errWindowClosed(); err != desktop.ErrWindowClosed {
		t.Fatalf("closed refusal = %#v, want shared not_found error", err)
	}
}
