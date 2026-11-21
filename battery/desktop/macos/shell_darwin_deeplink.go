//go:build darwin && (arm64 || amd64)

package macos

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const (
	aeGetURLClass = 0x4755524C // 'GURL'
	aeGetURLID    = 0x4755524C // 'GURL'
	aeKeyDirect   = 0x2D2D2D2D // '----'
)

func deepLinkBridgeMethods() []objc.Method {
	return []objc.Method{{
		Sel:   "handleGetURLEvent:withReplyEvent:",
		Types: "v@:@@",
		Fn:    ffi.NewCallback(bridgeGetURLEvent),
	}}
}

var deepLinkOpen atomic.Pointer[func(rawURL string)]

func installDeepLinkHandler(bridge objc.ID, onOpen func(rawURL string)) {
	if onOpen != nil {
		deepLinkOpen.Store(&onOpen)
	}
	mgr := objc.ID(objc.Send(objc.Class("NSAppleEventManager"), objc.Sel("sharedAppleEventManager")))
	objc.Send(mgr, objc.Sel("setEventHandler:andSelector:forEventClass:andEventID:"),
		uintptr(bridge), objc.Sel("handleGetURLEvent:withReplyEvent:"),
		uintptr(aeGetURLClass), uintptr(aeGetURLID))
}

func bridgeGetURLEvent(a *ffi.Args) uintptr {
	var raw string
	if event := objc.ID(a.Int[2]); event != 0 {
		if d := objc.Send(event, objc.Sel("paramDescriptorForKeyword:"), uintptr(aeKeyDirect)); d != 0 {
			raw = objc.GoString(objc.ID(objc.Send(objc.ID(d), objc.Sel("stringValue"))))
		}
	}
	if raw == "" {
		return 0
	}
	if deepLinkOpen.Load() != nil {
		deepLinks.push(raw)
	}
	return 0
}

const maxPendingDeepLinks = 16

var deepLinks deepLinkLine

type deepLinkLine struct {
	mu       sync.Mutex
	pending  []string
	draining bool
}

func (l *deepLinkLine) push(raw string) {
	l.mu.Lock()
	if len(l.pending) == maxPendingDeepLinks {
		l.pending = l.pending[1:]
	}
	l.pending = append(l.pending, raw)
	start := !l.draining
	l.draining = true
	l.mu.Unlock()
	if start {
		go l.drain()
	}
}

func (l *deepLinkLine) drain() {
	for {
		l.mu.Lock()
		if len(l.pending) == 0 {
			l.draining = false
			l.mu.Unlock()
			return
		}
		raw := l.pending[0]
		l.pending = l.pending[1:]
		l.mu.Unlock()
		if fn := deepLinkOpen.Load(); fn != nil {
			deliverDeepLink(raw, *fn)
		}
	}
}

func deliverDeepLink(raw string, fn func(string)) {
	defer func() {
		if v := recover(); v != nil {
			slog.Default().Error("desktop/macos: deep-link callback panicked", "panic", textsafe.Recovered(v))
		}
	}()
	fn(raw)
}
