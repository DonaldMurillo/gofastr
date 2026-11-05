package desktop

import "testing"

func TestClosedWindowErrorCode(t *testing.T) {
	if ErrWindowClosed.Code != CodeNotFound || ErrWindowClosed.Message != "window is closed" {
		t.Fatalf("closed-window contract = %#v, want not_found with a fixed message", ErrWindowClosed)
	}
}
