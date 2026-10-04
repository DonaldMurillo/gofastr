//go:build windows && amd64

package win32

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestSendKeyboardShortcutWrapsPartialSendError(t *testing.T) {
	original := sendInputCall
	t.Cleanup(func() { sendInputCall = original })
	wantErr := syscall.Errno(5)
	var requested uintptr
	sendInputCall = func(count uintptr, _ unsafe.Pointer, _ uintptr) (uintptr, error) {
		requested = count
		return count - 1, wantErr
	}

	err := SendKeyboardShortcut(0x41, 0x11)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "SendInput") {
		t.Fatalf("SendKeyboardShortcut error = %v, want wrapped SendInput error %v", err, wantErr)
	}
	if requested != 4 {
		t.Fatalf("requested events = %d, want 4", requested)
	}
}
