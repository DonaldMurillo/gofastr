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

// fakeWindowTitle answers GetWindowTextLengthW / GetWindowTextW for a
// title; grow, when set, replaces the title just before the first read
// (the title changed between the length query and the read).
type fakeWindowTitle struct {
	title, grow []uint16
	reads       int
}

func newFakeWindowTitle(title, grow string) *fakeWindowTitle {
	units := func(s string) []uint16 {
		u := syscall.StringToUTF16(s)
		return u[:len(u)-1]
	}
	f := &fakeWindowTitle{title: units(title)}
	if grow != "" {
		f.grow = units(grow)
	}
	return f
}

func (f *fakeWindowTitle) length() int { return len(f.title) }

func (f *fakeWindowTitle) read(buf []uint16) int {
	f.reads++
	if f.reads == 1 && f.grow != nil {
		f.title = f.grow
	}
	n := copy(buf[:len(buf)-1], f.title)
	buf[n] = 0
	return n
}

func TestWindowTextPast1024Units(t *testing.T) {
	long := strings.Repeat("é", 3000)
	f := newFakeWindowTitle(long, "")
	if got := readWindowText(f.length, f.read); got != long {
		t.Fatalf("read %d runes, want 3000", len([]rune(got)))
	}
	empty := newFakeWindowTitle("", "")
	if got := readWindowText(empty.length, empty.read); got != "" {
		t.Fatalf("empty title read as %q", got)
	}
}

func TestWindowTextRetriesWhenTitleGrew(t *testing.T) {
	f := newFakeWindowTitle("short", "a much longer title")
	if got := readWindowText(f.length, f.read); got != "a much longer title" {
		t.Fatalf("got %q, want the grown title", got)
	}
	if f.reads != 2 {
		t.Fatalf("reads = %d, want one retry", f.reads)
	}
}
