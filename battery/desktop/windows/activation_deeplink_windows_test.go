//go:build windows && amd64

package windows

import (
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// copyDataFor builds the WM_COPYDATA payload the second instance sends.
// The returned slice must stay alive while the message is read.
func copyDataFor(t *testing.T, rawURL string) (*copyDataMessage, []uint16) {
	t.Helper()
	wide, err := syscall.UTF16FromString(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return &copyDataMessage{Tag: copyDataDeepLink, Bytes: uint32(len(wide) * 2), Data: uintptr(unsafe.Pointer(&wide[0]))}, wide
}

func receiveFor(t *testing.T, s *winShell, hwnd uintptr, rawURL string) uintptr {
	t.Helper()
	msg, wide := copyDataFor(t, rawURL)
	got := s.receiveDeepLink(hwnd, uintptr(unsafe.Pointer(msg)))
	runtime.KeepAlive(wide)
	return got
}

// TestReceiveDeepLinkOnAnyAppWindow: every window the shell opens
// shares one class, and the second instance forwards to whichever one
// FindWindow returns first in Z-order. A Settings window or a widget in
// front must accept the URL like the main window does; a window this
// shell does not own must not.
func TestReceiveDeepLinkOnAnyAppWindow(t *testing.T) {
	const mainHwnd, settingsHwnd, widgetHwnd, foreignHwnd = 0x100, 0x200, 0x300, 0x400
	got := make(chan string, 4)
	s := &winShell{
		hwnd:           mainHwnd,
		deepLinkScheme: "notes",
		onDeepLink:     func(raw string) { got <- raw },
		windowsHWND: map[uintptr]*winWindow{
			mainHwnd:     {id: "main"},
			settingsHwnd: {id: "settings"},
			widgetHwnd:   {id: "w2"},
		},
	}
	for _, hwnd := range []uintptr{mainHwnd, settingsHwnd, widgetHwnd} {
		if r := receiveFor(t, s, hwnd, "notes://a/b"); r != 1 {
			t.Fatalf("receiveDeepLink on app window %#x = %d, want 1", hwnd, r)
		}
		select {
		case raw := <-got:
			if raw != "notes://a/b" {
				t.Fatalf("delivered %q", raw)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("URL received on %#x never reached OnDeepLink", hwnd)
		}
	}
	if r := receiveFor(t, s, foreignHwnd, "notes://a/b"); r != 0 {
		t.Fatalf("receiveDeepLink on a window the shell does not own = %d, want 0", r)
	}
	if r := receiveFor(t, s, 0, "notes://a/b"); r != 0 {
		t.Fatalf("receiveDeepLink on hwnd 0 = %d, want 0", r)
	}
}

// TestActivationTargetsMainWindow: the focus message that follows a
// forward brings the main window forward, not the Settings window or
// widget the second instance happened to find.
func TestActivationTargetsMainWindow(t *testing.T) {
	s := &winShell{hwnd: 0x100}
	if got := s.activationTarget(0x200); got != 0x100 {
		t.Fatalf("activationTarget(settings) = %#x, want the main window 0x100", got)
	}
	if got := (&winShell{}).activationTarget(0x200); got != 0x200 {
		t.Fatalf("activationTarget with no main window = %#x, want the window reached", got)
	}
}

// TestReceiveDeepLinkKeepsArrivalOrder: a forward of two URLs arrives
// as two WM_COPYDATA messages on the UI thread. The second must not
// reach OnDeepLink while the first is still in the callback, and the
// two arrive in order.
func TestReceiveDeepLinkKeepsArrivalOrder(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	s := &winShell{
		hwnd:           0x100,
		deepLinkScheme: "notes",
		windowsHWND:    map[uintptr]*winWindow{0x100: {id: "main"}},
		onDeepLink: func(raw string) {
			mu.Lock()
			order = append(order, raw)
			mu.Unlock()
			if raw == "notes://first" {
				once.Do(func() { close(entered) })
				<-release
			}
		},
	}
	if r := receiveFor(t, s, 0x100, "notes://first"); r != 1 {
		t.Fatalf("first receive = %d", r)
	}
	<-entered
	if r := receiveFor(t, s, 0x100, "notes://second"); r != 1 {
		t.Fatalf("second receive = %d", r)
	}
	time.Sleep(50 * time.Millisecond) // a goroutine per message would deliver now
	mu.Lock()
	early := len(order)
	mu.Unlock()
	close(release)
	if early != 1 {
		t.Fatalf("OnDeepLink ran %d times while the first call was still running, want 1", early)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "notes://first" || order[1] != "notes://second" {
		t.Fatalf("delivery order = %q, want [notes://first notes://second]", order)
	}
}

// TestDeepLinkLineSurvivesPanic: a panicking host callback drops that
// URL; the next one still arrives.
func TestDeepLinkLineSurvivesPanic(t *testing.T) {
	got := make(chan string, 1)
	var l deepLinkLine
	deliver := func(raw string) {
		if raw == "notes://boom" {
			panic("host callback")
		}
		got <- raw
	}
	l.push("notes://boom", deliver)
	l.push("notes://next", deliver)
	select {
	case raw := <-got:
		if raw != "notes://next" {
			t.Fatalf("delivered %q", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the URL after a panicking callback was never delivered")
	}
}

// TestDeepLinkLineDropsOldestPastBound: a stalled host callback cannot
// grow the line without bound. Past maxPendingDeepLinks the oldest
// waiting URL is dropped.
func TestDeepLinkLineDropsOldestPastBound(t *testing.T) {
	var (
		mu  sync.Mutex
		got []string
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	deliver := func(raw string) {
		if raw == "stall" {
			once.Do(func() { close(entered) })
			<-release
		}
		mu.Lock()
		got = append(got, raw)
		mu.Unlock()
	}
	var l deepLinkLine
	l.push("stall", deliver)
	<-entered
	for i := range maxPendingDeepLinks + 1 {
		l.push(string(rune('a'+i)), deliver)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		idle := !l.draining
		l.mu.Unlock()
		if idle || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != maxPendingDeepLinks+1 || got[0] != "stall" || got[1] != "b" {
		t.Fatalf("delivered %q, want stall then b.. (a dropped)", got)
	}
}
