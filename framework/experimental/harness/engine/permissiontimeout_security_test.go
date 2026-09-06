package engine

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. PermissionMiddleware tested its timeout
// `<= 0` and substituted 60s, so a negative permission timeout (sign or
// unit error, e.g. a profile's permission_timeout computed negative)
// silently became the LONGEST wait for a human ack — the exact opposite
// of the fail-fast the caller asked for.
// Surfaces: PermissionMiddleware (the ask-ack timeout).
// Pins timeout <= 0 folding onto the 60s default, found by the
// 2026-09-04 red-probe round; fixed in PermissionMiddleware panicking at
// construction for timeout < 0 while 0 keeps the default.

func TestPermissionMwNegativeTimeoutPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("PermissionMiddleware: negative timeout silently folded onto the 60s default")
		}
	}()
	_ = PermissionMiddleware(nil, nil, nil, ids.SessionID("s"), -time.Second)
}

func TestPermissionMwZeroTimeoutKeepsDefault(t *testing.T) {
	mw := PermissionMiddleware(nil, nil, nil, ids.SessionID("s"), 0)
	if mw == nil {
		t.Fatal("zero timeout must keep the 60s default, not fail construction")
	}
}
